package core

import (
	"errors"
	"github.com/hashicorp/go-hclog"
	"github.com/seafooler/bdt/config"
	"github.com/seafooler/bdt/conn"
	"reflect"
	"sync"
	"time"
)

type Node struct {
	*config.Config
	Bolt  *Bolt
	Aba   *ABA
	Smvba *SMVBA

	logger hclog.Logger

	reflectedTypesMap map[uint8]reflect.Type

	trans *conn.NetworkTransport

	paceSyncMsgsReceived map[int]struct{}
	paceSyncMsgSent      bool

	status uint8 // 0 or 1 indicates the node is in the status of bolt or aba

	sync.Mutex
}

func NewNode(conf *config.Config) *Node {
	node := &Node{
		Config:               conf,
		reflectedTypesMap:    reflectedTypesMap,
		paceSyncMsgsReceived: make(map[int]struct{}),
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
	for {
		select {
		case msg := <-msgCh:
			switch msgAsserted := msg.(type) {
			case BoltProposalMsg:
				if n.status == 0 {
					go n.Bolt.ProcessBoltProposalMsg(&msgAsserted)
				}
			case BoltVoteMsg:
				if n.status == 0 {
					go n.Bolt.ProcessBoltVoteMsg(&msgAsserted)
				}
			case ABABvalRequestMsg:
				if n.status == 1 {
					go n.Aba.handleBvalRequest(&msgAsserted)
				}
			case ABAAuxRequestMsg:
				if n.status == 1 {
					go n.Aba.handleAuxRequest(&msgAsserted)
				}
			case ABAExitMsg:
				if n.status == 1 {
					go n.Aba.handleExitMessage(&msgAsserted)
				}
			case PaceSyncMsg:
				n.logger.Info("Receive a pace sync message", "msg", msgAsserted)
				go n.handlePaceSyncMessage(&msgAsserted)
			case SMVBAPBVALMessage:
				if n.status == 2 {
					go n.Smvba.spb.processPBVALMsg(&msgAsserted)
				}
			case SMVBAPBVOTMessage:
				if n.status == 2 {
					go n.Smvba.spb.processPBVOTMsg(&msgAsserted)
				}
			case SMVBAFinishMessage:
				if n.status == 2 {
					go n.Smvba.HandleFinishMsg(&msgAsserted)
				}
			case SMVBADoneShareMessage:
				if n.status == 2 {
					go n.Smvba.HandleDoneShareMsg(&msgAsserted)
				}
			case SMVBAPreVoteMessage:
				if n.status == 2 {
					go n.Smvba.HandlePreVoteMsg(&msgAsserted)
				}
			case SMVBAVoteMessage:
				if n.status == 2 {
					go n.Smvba.HandleVoteMsg(&msgAsserted)
				}
			case SMVBAHaltMessage:
				if n.status == 2 {
					go n.Smvba.HandleHaltMsg(&msgAsserted)
				}
			default:
				n.logger.Error("Unknown type of the received message!")
			}
		case <-time.After(time.Second * time.Duration(n.Timeout)):
			n.Lock()
			// timeout only works when the node is in Bolt
			if n.status != 0 {
				continue
				n.Unlock()
			}
			if !n.paceSyncMsgSent {
				n.logger.Info("Broadcast a pace sync message")
				go n.PlainBroadcast(PaceSyncMsgTag, PaceSyncMsg{Sender: n.Id, Epoch: n.Bolt.maxProofedHeight}, nil)
				n.paceSyncMsgSent = true
			}
			n.Unlock()
		}
	}
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

func (n *Node) handlePaceSyncMessage(t *PaceSyncMsg) {
	n.Lock()
	defer n.Unlock()
	n.paceSyncMsgsReceived[t.Sender] = struct{}{}
	/* No amplification process of pace sync messages
	if len(n.paceSyncMsgsReceived) >= n.F+1 && !n.paceSyncMsgSent {
		n.logger.Info("Broadcast a timeout message")
		go n.PlainBroadcast(PaceSyncMsgTag, PaceSyncMsg{Sender: n.Id, Epoch: n.Bolt.maxProofedHeight}, nil)
	}

	n.paceSyncMsgSent = true
	*/

	if len(n.paceSyncMsgsReceived) >= 2*n.F+1 && n.status == 0 {
		n.logger.Info("Switch from Bolt to ABA after timeout being triggered")
		n.status = 1
		n.Aba.inputValue(n.Bolt.maxProofedHeight)
	}
}
