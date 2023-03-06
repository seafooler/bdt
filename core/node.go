package core

import (
	"errors"
	"github.com/hashicorp/go-hclog"
	"github.com/seafooler/bdt/config"
	"github.com/seafooler/bdt/conn"
	"math"
	"reflect"
	"sync"
	"time"
)

const STATUSCOUNT = 3

type Node struct {
	*config.Config
	sn    int
	Bolt  *Bolt
	Aba   *ABA
	Smvba *SMVBA

	logger hclog.Logger

	reflectedTypesMap map[uint8]reflect.Type

	trans *conn.NetworkTransport

	status             uint8 // 0, 1, 2 indicates the node is in the status of bolt, aba, or smvba
	statusChangeSignal chan StatusChangeSignal

	cachedMsgs map[int][3][]interface{} // cache the messages arrived in advance
	timer      *time.Timer

	// mock the transactions sent from clients
	lastBlockCreatedTime time.Time
	maxCachedTxs         int

	sync.Mutex
}

func NewNode(conf *config.Config) *Node {
	node := &Node{
		Config:             conf,
		reflectedTypesMap:  reflectedTypesMap,
		statusChangeSignal: make(chan StatusChangeSignal),
		cachedMsgs:         make(map[int][3][]interface{}),
		// timer will be reset in message loop
		timer:        time.NewTimer(time.Duration(math.MaxInt32) * time.Second),
		maxCachedTxs: conf.MaxPayloadCount * (conf.MaxPayloadSize / conf.TxSize),
	}

	node.logger = hclog.New(&hclog.LoggerOptions{
		Name:   "bdt-node",
		Output: hclog.DefaultOutput,
		Level:  hclog.Level(node.Config.LogLevel),
	})

	node.Bolt = NewBolt(node, 0)
	node.Aba = NewABA(node)
	node.Smvba = NewSMVBA(node)
	return node
}

// StartP2PListen starts the node to listen for P2P connection.
func (n *Node) StartP2PListen() error {
	var err error
	n.trans, err = conn.NewTCPTransport(":"+n.P2pPort, 2*time.Second,
		nil, n.MaxPool, n.reflectedTypesMap)
	if err != nil {
		return err
	}
	return nil
}

// HandleMsgsLoop starts a loop to deal with the msgs from other peers.
func (n *Node) HandleMsgsLoop() {
	msgCh := n.trans.MsgChan()
	n.timer.Reset(time.Duration(n.Timeout) * time.Millisecond)
	for {
		select {
		case msg := <-msgCh:
			switch msgAsserted := msg.(type) {
			case BoltProposalMsg:
				if n.processItNow(msgAsserted.SN, 0, msgAsserted) {
					n.timer.Reset(time.Duration(n.Timeout) * time.Millisecond)
					go n.Bolt.ProcessBoltProposalMsg(&msgAsserted)
				}
			case BoltVoteMsg:
				if n.processItNow(msgAsserted.SN, 0, msgAsserted) {
					go n.Bolt.ProcessBoltVoteMsg(&msgAsserted)
				}
			case PaceSyncMsg:
				n.logger.Debug("Receive a pace sync message", "msg", msgAsserted)
				if n.processItNow(msgAsserted.SN, 0, msgAsserted) {
					go n.Bolt.handlePaceSyncMessage(&msgAsserted)
				}
			case ABABvalRequestMsg:
				if n.processItNow(msgAsserted.SN, 1, msgAsserted) {
					go n.Aba.handleBvalRequest(&msgAsserted)
				}
			case ABAAuxRequestMsg:
				if n.processItNow(msgAsserted.SN, 1, msgAsserted) {
					go n.Aba.handleAuxRequest(&msgAsserted)
				}
			case ABAExitMsg:
				if n.processItNow(msgAsserted.SN, 1, msgAsserted) {
					go n.Aba.handleExitMessage(&msgAsserted)
				}
			case SMVBAPBVALMessage:
				n.logger.Debug("Receive SMVBAPBVALMessage", "msg", msgAsserted)
				if n.processItNow(msgAsserted.SN, 2, msgAsserted) {
					go n.Smvba.spb.processPBVALMsg(&msgAsserted)
				}
			case SMVBAPBVOTMessage:
				n.logger.Debug("Receive SMVBAPBVOTMessage", "msg", msgAsserted)
				if n.processItNow(msgAsserted.SN, 2, msgAsserted) {
					go n.Smvba.spb.processPBVOTMsg(&msgAsserted)
				}
			case SMVBAFinishMessage:
				n.logger.Debug("Receive SMVBAFinishMessage", "msg", msgAsserted)
				if n.processItNow(msgAsserted.SN, 2, msgAsserted) {
					go n.Smvba.HandleFinishMsg(&msgAsserted)
				}
			case SMVBADoneShareMessage:
				n.logger.Debug("Receive SMVBADoneShareMessage", "msg", msgAsserted)
				if n.processItNow(msgAsserted.SN, 2, msgAsserted) {
					go n.Smvba.HandleDoneShareMsg(&msgAsserted)
				}
			case SMVBAPreVoteMessage:
				n.logger.Debug("Receive SMVBAPreVoteMessage", "msg", msgAsserted)
				if n.processItNow(msgAsserted.SN, 2, msgAsserted) {
					go n.Smvba.HandlePreVoteMsg(&msgAsserted)
				}
			case SMVBAVoteMessage:
				n.logger.Debug("Receive SMVBAVoteMessage", "msg", msgAsserted)
				if n.processItNow(msgAsserted.SN, 2, msgAsserted) {
					go n.Smvba.HandleVoteMsg(&msgAsserted)
				}
			case SMVBAHaltMessage:
				n.logger.Debug("Receive SMVBAHaltMessage", "msg", msgAsserted)
				if n.processItNow(msgAsserted.SN, 2, msgAsserted) {
					go n.Smvba.HandleHaltMsg(&msgAsserted)
				}
			default:
				n.logger.Error("Unknown type of the received message!")
			}
		case <-n.timer.C:
			n.logger.Info("timeout ...........................")
			n.Lock()
			n.logger.Info("Acquire the lock in timeout ...........................", "n.status", n.status)
			// timeout only works when the node is in Bolt
			if n.status != 0 {
				n.Unlock()
				continue
			}
			go n.Bolt.TriggerPaceSync()
			n.Unlock()
		case scs := <-n.statusChangeSignal:
			n.logger.Info("Receive a status change signal", "cur_status", n.status, "to_status", scs.Status)
			n.Lock()
			if scs.SN != n.sn {
				continue
			}
			if (n.status+1)%STATUSCOUNT == scs.Status {
				n.status = scs.Status
				switch n.status {
				case 1:
					n.Aba = NewABA(n)
					n.restoreMessages(1)
					go n.Aba.inputValue(n.Bolt.maxProofedHeight)
				case 2:
					n.Smvba = NewSMVBA(n)
					n.restoreMessages(2)
					curTime := time.Now()
					estimatdTxNum := int(curTime.Sub(n.lastBlockCreatedTime).Seconds() * float64(n.Config.Rate))
					if estimatdTxNum > n.maxCachedTxs {
						estimatdTxNum = n.maxCachedTxs
					}

					n.lastBlockCreatedTime = curTime

					go n.Smvba.RunOneMVBAView(false, NewTxBatch(n.MaxPayloadSize), nil, estimatdTxNum, -1)
				case 0:
					n.sn = n.sn + 1
					lastBoltCommittedHeight := n.Bolt.committedHeight
					n.Bolt = NewBolt(n, 0)
					n.restoreMessages(0)
					n.timer.Reset(time.Duration(n.Timeout) * time.Millisecond)
					go n.Bolt.ProposalLoop(lastBoltCommittedHeight + 4) // multiples of 50
				}
			}
			n.Unlock()
		}
	}
}

// processItNow caches messages from the future SNs or stages or ignore obsolete messages
// processItNow must be called in a concurrent-safe environment
func (n *Node) processItNow(msgSN int, msgStatus uint8, msg interface{}) bool {
	if msgSN > n.sn || (msgSN == n.sn && msgStatus > n.status) {
		if _, ok := n.cachedMsgs[msgSN]; !ok {
			n.cachedMsgs[msgSN] = [3][]interface{}{}
		}
		cache := n.cachedMsgs[msgSN]
		cache[msgStatus] = append(cache[msgStatus], msg)
		n.cachedMsgs[msgSN] = cache
		return false
	}

	if msgSN < n.sn || (msgSN == n.sn && msgStatus < n.status) {
		// if receiving an obsolete message, ignore it
		return false
	}

	return true
}

// restoreMessages process the cached messages from cachedMsgs
// restoreMessages must be called in a concurrent-safe environment
func (n *Node) restoreMessages(status uint8) {
	if _, ok := n.cachedMsgs[n.sn]; !ok {
		return
	}
	msgs := n.cachedMsgs[n.sn][status]
	for _, msg := range msgs {
		switch msgAsserted := msg.(type) {
		case BoltProposalMsg:
			go n.Bolt.ProcessBoltProposalMsg(&msgAsserted)
		case BoltVoteMsg:
			go n.Bolt.ProcessBoltVoteMsg(&msgAsserted)
		case PaceSyncMsg:
			go n.Bolt.handlePaceSyncMessage(&msgAsserted)
		case ABABvalRequestMsg:
			go n.Aba.handleBvalRequest(&msgAsserted)
		case ABAAuxRequestMsg:
			go n.Aba.handleAuxRequest(&msgAsserted)
		case ABAExitMsg:
			go n.Aba.handleExitMessage(&msgAsserted)
		case SMVBAPBVALMessage:
			go n.Smvba.spb.processPBVALMsg(&msgAsserted)
		case SMVBAPBVOTMessage:
			go n.Smvba.spb.processPBVOTMsg(&msgAsserted)
		case SMVBAFinishMessage:
			go n.Smvba.HandleFinishMsg(&msgAsserted)
		case SMVBADoneShareMessage:
			go n.Smvba.HandleDoneShareMsg(&msgAsserted)
		case SMVBAPreVoteMessage:
			go n.Smvba.HandlePreVoteMsg(&msgAsserted)
		case SMVBAVoteMessage:
			go n.Smvba.HandleVoteMsg(&msgAsserted)
		case SMVBAHaltMessage:
			go n.Smvba.HandleHaltMsg(&msgAsserted)
			break
		}
	}
	cache := n.cachedMsgs[n.sn]
	cache[status] = []interface{}{}
	n.cachedMsgs[n.sn] = cache
}

// EstablishP2PConns establishes P2P connections with other nodes.
func (n *Node) EstablishP2PConns() error {
	if n.trans == nil {
		return errors.New("networktransport has not been created")
	}
	for name, addr := range n.Id2AddrMap {
		addrWithPort := addr + ":" + n.Id2PortMap[name]
		conn, err := n.trans.GetConn(addrWithPort)
		if err != nil {
			return err
		}
		n.trans.ReturnConn(conn)
		n.logger.Debug("connection has been established", "sender", n.Name, "receiver", addr)
	}
	return nil
}

// SendMsg sends a message to another peer identified by the addrPort (e.g., 127.0.0.1:7788)
func (n *Node) SendMsg(tag byte, data interface{}, sig []byte, addrPort string) error {
	c, err := n.trans.GetConn(addrPort)
	if err != nil {
		return err
	}
	time.Sleep(time.Millisecond * time.Duration(n.Config.MockLatency))
	if err := conn.SendMsg(c, tag, data, sig); err != nil {
		return err
	}

	if err = n.trans.ReturnConn(c); err != nil {
		return err
	}
	return nil
}

// PlainBroadcast broadcasts data in its best effort
func (n *Node) PlainBroadcast(tag byte, data interface{}, sig []byte) error {
	for id, addr := range n.Id2AddrMap {
		port := n.Id2PortMap[id]
		addrPort := addr + ":" + port
		err := n.SendMsg(tag, data, sig, addrPort)
		if err != nil {
			return err
		}
	}
	return nil
}
