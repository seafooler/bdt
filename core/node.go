package core

import (
	"errors"
	"fmt"
	"github.com/hashicorp/go-hclog"
	"github.com/seafooler/bdt/config"
	"github.com/seafooler/bdt/conn"
	"github.com/valyala/gorpc"
	"log"
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

	payLoadTrans      *conn.NetworkTransport
	rpcClientsMap     map[int]*gorpc.Client
	payLoads          map[[HASHSIZE]byte]bool
	committedPayloads map[[HASHSIZE]byte]bool
	proposedPayloads  map[[HASHSIZE]byte]bool
	maxNumInPayLoad   int

	status             uint8 // 0, 1, 2 indicates the node is in the status of bolt, aba, or smvba
	statusChangeSignal chan StatusChangeSignal

	cachedMsgs map[int][3][]interface{} // cache the messages arrived in advance
	timer      *time.Timer

	// mock the transactions sent from clients
	//lastBlockCreatedTime time.Time
	//maxCachedTxs         int

	payLoadTxNum int

	sync.Mutex
}

func NewNode(conf *config.Config) *Node {
	node := &Node{
		Config:             conf,
		reflectedTypesMap:  reflectedTypesMap,
		statusChangeSignal: make(chan StatusChangeSignal),
		cachedMsgs:         make(map[int][3][]interface{}),
		// timer will be reset in message loop
		timer: time.NewTimer(time.Duration(math.MaxInt32) * time.Second),
		//maxCachedTxs: conf.MaxPayloadCount * (conf.MaxPayloadSize / conf.TxSize),
		maxNumInPayLoad:   conf.MaxPayloadSize / conf.TxSize,
		payLoads:          make(map[[HASHSIZE]byte]bool),
		committedPayloads: make(map[[HASHSIZE]byte]bool),
		proposedPayloads:  make(map[[HASHSIZE]byte]bool),
		rpcClientsMap:     make(map[int]*gorpc.Client),
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

//// StartP2PPayLoadListen starts the node to listen for P2P connection for payload broadcast.
//func (n *Node) StartP2PPayLoadListen() error {
//	var err error
//	n.payLoadTrans, err = conn.NewTCPTransport(":"+n.P2pPortPayload, 2*time.Second,
//		nil, n.MaxPool, n.reflectedTypesMap)
//	if err != nil {
//		return err
//	}
//	return nil
//}

func (n *Node) StartListenRPC() {
	gorpc.RegisterType(&PayLoadMsg{})
	s := &gorpc.Server{
		// Accept clients on this TCP address.
		Addr: ":" + n.P2pPortPayload,

		SendBufferSize: 100 * 1024 * 1024,

		RecvBufferSize: 100 * 1024 * 1024,

		Handler: func(clientAddr string, payLoad interface{}) interface{} {
			assertedPayLoad, ok := payLoad.(*PayLoadMsg)
			if !ok {
				panic("message send is not a payload")
			}
			n.Lock()
			defer n.Unlock()
			if _, ok := n.committedPayloads[assertedPayLoad.Hash]; ok {
				n.logger.Debug("Receive an already committed payload", "sender", assertedPayLoad.Sender, "hash",
					string(assertedPayLoad.Hash[:]))
			} else {
				n.payLoads[assertedPayLoad.Hash] = true
				n.logger.Debug("Receive a payload", "sender", assertedPayLoad.Sender, "hash",
					string(assertedPayLoad.Hash[:]), "payload count", len(n.payLoads))
			}
			return nil
		},
	}
	if err := s.Serve(); err != nil {
		log.Fatalf("Cannot start rpc server: %s", err)
	}
}

// BroadcastPayLoad mocks the underlying payload broadcast
func (n *Node) BroadcastPayLoadLoop() {
	gorpc.RegisterType(&PayLoadMsg{})
	payLoadFullTime := float32(n.Config.MaxPayloadSize) / float32(n.Config.TxSize*n.Rate)

	n.logger.Info("payloadFullTime", "s", payLoadFullTime)

	for {
		time.Sleep(time.Duration(payLoadFullTime*1000) * time.Millisecond)
		txNum := int(float32(n.Rate) * payLoadFullTime)
		mockHash, err := genMsgHashSum([]byte(fmt.Sprintf("%d%v", n.Id, time.Now())))
		if err != nil {
			panic(err)
		}
		start := time.Now()
		var buf [HASHSIZE]byte
		copy(buf[0:HASHSIZE], mockHash[:])
		payLoadMsg := PayLoadMsg{
			Sender: n.Name,
			// Mock a hash
			Hash: buf,
			Reqs: make([][]byte, txNum),
		}
		n.logger.Debug("1st step takes", "ms", time.Now().Sub(start).Milliseconds())
		start = time.Now()
		for i := 0; i < txNum; i++ {
			payLoadMsg.Reqs[i] = make([]byte, n.Config.TxSize)
			payLoadMsg.Reqs[i][n.Config.TxSize-1] = '0'
		}
		n.logger.Debug("2nd step takes", "ms", time.Now().Sub(start).Milliseconds())
		n.BroadcastPayLoad(payLoadMsg, buf[:])
		time.Sleep(time.Millisecond * 100)
	}
}

//func (n *Node) HandlePayLoadMsgsLoop() {
//	msgCh := n.payLoadTrans.MsgChan()
//
//	for {
//		select {
//		case msg := <-msgCh:
//			switch msgAsserted := msg.(type) {
//			case PayLoadMsg:
//				n.Lock()
//				if _, ok := n.committedPayloads[msgAsserted.Hash]; ok {
//					n.logger.Info("Receive an already committed payload", "sender", msgAsserted.Sender, "hash",
//						string(msgAsserted.Hash[:]))
//				} else {
//					n.payLoads[msgAsserted.Hash] = true
//					n.logger.Info("Receive a payload", "sender", msgAsserted.Sender, "hash",
//						string(msgAsserted.Hash[:]), "payload count", len(n.payLoads))
//				}
//				n.Unlock()
//			default:
//				n.logger.Error("Unknown type of the received message from payload transportion!")
//			}
//		}
//	}
//}

// HandleMsgsLoop starts a loop to deal with the msgs from other peers.
func (n *Node) HandleMsgsLoop() {
	msgCh := n.trans.MsgChan()
	fmt.Printf("Timeout: %d, MockLatency: %d\n", n.Timeout, n.MockLatency)

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
					//curTime := time.Now()
					//estimatdTxNum := int(curTime.Sub(n.lastBlockCreatedTime).Seconds() * float64(n.Config.Rate))
					//if estimatdTxNum > n.maxCachedTxs {
					//	estimatdTxNum = n.maxCachedTxs
					//}
					//
					//n.lastBlockCreatedTime = curTime

					payLoadHashes, cnt := n.createBlock()

					go n.Smvba.RunOneMVBAView(false, payLoadHashes, nil, cnt*n.maxNumInPayLoad, -1)
				case 0:
					n.sn = n.sn + 1
					lastBoltCommittedHeight := n.Bolt.committedHeight
					n.Bolt = NewBolt(n, 0)
					n.restoreMessages(0)
					n.timer.Reset(time.Duration(n.Timeout) * time.Millisecond)
					go n.Bolt.ProposalLoop(lastBoltCommittedHeight + 1)
				}
			}
			n.Unlock()
		}
	}
}

// createBlock must be called in a concurrent context
func (n *Node) createBlock() ([][HASHSIZE]byte, int) {
	var payLoadHashes [][HASHSIZE]byte
	payLoadCount := len(n.payLoads)
	if payLoadCount < n.MaxPayloadCount {
		payLoadHashes = make([][HASHSIZE]byte, payLoadCount)
	} else {
		payLoadHashes = make([][HASHSIZE]byte, n.MaxPayloadCount)
	}
	i := 0
	for ph, _ := range n.payLoads {
		if _, ok := n.proposedPayloads[ph]; !ok {
			payLoadHashes[i] = ph
		}
		i++
		if i >= len(payLoadHashes) {
			break
		}
	}
	return payLoadHashes, i - 1
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

//// EstablishP2PPayloadConns establishes P2P connections for payloads with other nodes.
//func (n *Node) EstablishP2PPayloadConns() error {
//	if n.payLoadTrans == nil {
//		return errors.New("networktransport has not been created")
//	}
//	for name, addr := range n.Id2AddrMap {
//		addrWithPort := addr + ":" + n.Id2PortPayLoadMap[name]
//		conn, err := n.payLoadTrans.GetConn(addrWithPort)
//		if err != nil {
//			return err
//		}
//		n.payLoadTrans.ReturnConn(conn)
//		n.logger.Debug("connection has been established", "sender", n.Name, "receiver", addr)
//	}
//	return nil
//}

func (n *Node) EstablishRPCConns() {
	for name, addr := range n.Id2AddrMap {
		addrWithPort := addr + ":" + n.Id2PortPayLoadMap[name]
		c := &gorpc.Client{
			Addr:           addrWithPort,
			RequestTimeout: 100 * time.Second,
			SendBufferSize: 100 * 1024 * 1024,
			RecvBufferSize: 100 * 1024 * 1024,
		}
		n.rpcClientsMap[name] = c
		c.Start()
	}
}

// SendMsg sends a message to another peer identified by the addrPort (e.g., 127.0.0.1:7788)
func (n *Node) SendMsg(tag byte, data interface{}, sig []byte, addrPort string) error {
	start := time.Now()
	c, err := n.trans.GetConn(addrPort)
	n.logger.Debug("Get a connection costs", "ms", time.Now().Sub(start).Milliseconds(),
		"tag", tag)
	if err != nil {
		return err
	}
	time.Sleep(time.Millisecond * time.Duration(n.Config.MockLatency))
	start = time.Now()
	if err := conn.SendMsg(c, tag, data, sig); err != nil {
		return err
	}
	n.logger.Debug("Sending message", "ms", time.Now().Sub(start).Milliseconds(),
		"tag", tag)

	if err = n.trans.ReturnConn(c); err != nil {
		return err
	}
	return nil
}

//// SendPayLoad sends a payload to another peer identified by the addrPort (e.g., 127.0.0.1:7788)
//func (n *Node) SendPayLoad(tag byte, data interface{}, hash, sig []byte, addrPort string) error {
//	start := time.Now()
//	c, err := n.payLoadTrans.GetConn(addrPort)
//	n.logger.Info("Get a payload connection costs", "ms", time.Now().Sub(start).Milliseconds(),
//		"tag", tag)
//	if err != nil {
//		return err
//	}
//	start = time.Now()
//	if err := conn.SendMsg(c, tag, data, sig); err != nil {
//		return err
//	}
//	n.logger.Info("Sending a payload", "ms", time.Now().Sub(start).Milliseconds(),
//		"tag", tag, "hash", hash)
//
//	if err = n.payLoadTrans.ReturnConn(c); err != nil {
//		return err
//	}
//	return nil
//}

// PlainBroadcast broadcasts data in its best effort
func (n *Node) PlainBroadcast(tag byte, data interface{}, sig []byte) error {
	for i, a := range n.Id2AddrMap {
		go func(id int, addr string) {
			port := n.Id2PortMap[id]
			addrPort := addr + ":" + port
			start := time.Now()
			if err := n.SendMsg(tag, data, sig, addrPort); err != nil {
				panic(err)
			}
			n.logger.Debug("Broadcasting a message", "tag", tag, "ms", time.Now().Sub(start).Milliseconds())

		}(i, a)
	}
	return nil
}

// BroadcastPayLoad broadcasts the payload in its best effort
func (n *Node) BroadcastPayLoad(data interface{}, hash []byte) error {
	for i, _ := range n.Id2AddrMap {
		go func(id int) {
			start := time.Now()
			if _, err := n.rpcClientsMap[id].Call(data); err != nil {
				panic(err)
			}
			n.logger.Debug("Sending a payload", "ms", time.Now().Sub(start).Milliseconds(), "hash", string(hash))
		}(i)
	}
	return nil
}

// BroadcastSyncLaunchMsgs sends the PaceSyncMsg to help all the replicas launch simultaneously
func (n *Node) BroadcastSyncLaunchMsgs() error {
	for i, a := range n.Id2AddrMap {
		go func(id int, addr string) {
			port := n.Id2PortMap[id]
			addrPort := addr + ":" + port
			c, err := n.trans.GetConn(addrPort)
			if err != nil {
				panic(err)
			}
			if err := conn.SendMsg(c, PaceSyncMsgTag, PaceSyncMsg{SN: -1, Sender: n.Id, Epoch: -1}, nil); err != nil {
				panic(err)
			}
			if err = n.trans.ReturnConn(c); err != nil {
				panic(err)
			}
		}(i, a)
	}
	return nil
}

func (n *Node) WaitForEnoughSyncLaunchMsgs() error {
	msgCh := n.trans.MsgChan()
	count := 0
	for {
		select {
		case msg := <-msgCh:
			switch msg.(type) {
			case PaceSyncMsg:
				count += 1
				if count >= n.N-1 {
					return nil
				}
			default:
				continue
			}
		}
	}
	return nil
}
