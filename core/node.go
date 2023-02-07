package core

import (
	"errors"
	"github.com/hashicorp/go-hclog"
	"github.com/seafooler/bdt/config"
	"github.com/seafooler/bdt/conn"
	"reflect"
	"time"
)

type Node struct {
	*config.Config
	Bolt *Bolt
	Aba  *ABA

	logger hclog.Logger

	reflectedTypesMap map[uint8]reflect.Type

	trans *conn.NetworkTransport
}

func NewNode(conf *config.Config) *Node {
	node := &Node{
		Config:            conf,
		reflectedTypesMap: reflectedTypesMap,
	}

	node.logger = hclog.New(&hclog.LoggerOptions{
		Name:   "bdt-node",
		Output: hclog.DefaultOutput,
		Level:  hclog.Level(node.Config.LogLevel),
	})

	node.Bolt = NewBolt(node, 0)
	node.Aba = NewABA(node)
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
				go n.Bolt.ProcessBoltProposalMsg(&msgAsserted)
			case BoltVoteMsg:
				go n.Bolt.ProcessBoltVoteMsg(&msgAsserted)
			//case AgreementMessage:
			//	go n.Aba.handleMessage(&msgAsserted)
			case ABABvalRequestMsg:
				go n.Aba.handleBvalRequest(&msgAsserted)
			case ABAAuxRequestMsg:
				go n.Aba.handleAuxRequest(&msgAsserted)
			case ABAExitMsg:
				go n.Aba.handleExitMessage(&msgAsserted)
			default:
				n.logger.Error("Unknown type of the received message!")
			}
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
