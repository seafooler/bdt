package core

import (
	"crypto/ed25519"
	"fmt"
	"github.com/seafooler/bdt/config"
	"github.com/seafooler/sign_tools"
	"strconv"
	"testing"
	"time"
)

var id2NameMap = map[int]string{
	0: "node0",
	1: "node1",
	2: "node2",
	3: "node3",
}

var name2IdMap = map[string]int{
	"node0": 0,
	"node1": 1,
	"node2": 2,
	"node3": 3,
}

var id2AddrMap = map[int]string{
	0: "127.0.0.1",
	1: "127.0.0.1",
	2: "127.0.0.1",
	3: "127.0.0.1",
}

var id2PortMap = map[int]string{
	0: "8000",
	1: "8010",
	2: "8020",
	3: "8030",
}

var clusterAddr = map[string]string{
	"node0": "127.0.0.1",
	"node1": "127.0.0.1",
	"node2": "127.0.0.1",
	"node3": "127.0.0.1",
}
var clusterPort = map[string]string{
	"node0": "8000",
	"node1": "8010",
	"node2": "8020",
	"node3": "8030",
}

func setupNodes() []*Node {
	//ddos := []bool{false, false, false, true}
	names := []string{"node0", "node1", "node2", "node3"}

	var id2P2PPortPayloadMap = map[int]string{
		0: "7000",
		1: "7010",
		2: "7020",
		3: "7030",
	}

	shares, pubPoly := sign_tools.GenTSKeys(3, 4)

	sks := make([]ed25519.PrivateKey, 4)
	pksMap := make(map[int]ed25519.PublicKey, 4)
	for i := 0; i < 4; i++ {
		sk, pk := sign_tools.GenED25519Keys()
		sks[i] = sk
		pksMap[i] = pk
	}

	// create configs and nodes
	confs := make([]*config.Config, 4)
	nodes := make([]*Node, 4)

	for i := 0; i < 4; i++ {
		confs[i] = config.New(i, names[i], id2NameMap, name2IdMap, clusterAddr[names[i]], clusterPort[names[i]], id2P2PPortPayloadMap[i], shares[i], pubPoly,
			sks[i], pksMap, id2AddrMap, id2PortMap, id2P2PPortPayloadMap, 10, 3, 500, 100, true, 2000,
			1500, 100, 100, 100, 1)

		nodes[i] = NewNode(confs[i])
		nodes[i].ClientPort = strconv.Itoa(9000 + i*10)
		nodes[i].Client.Name = "Client" + strconv.Itoa(i)

		/*
			nodes[i].ClientPort = strconv.Itoa(9000 + i*10)
			nodes[i].Client.Name = "Client" + strconv.Itoa(i)
		*/
		if err := nodes[i].StartP2PListen(); err != nil {
			panic(err)
		}
		go nodes[i].StartListenRPC()
		go nodes[i].StartClientRPC()

		/*
			if err := nodes[i].StartP2PSyncListen(); err != nil {
				panic(err)
			}
		*/
	}

	for i := 0; i < 4; i++ {
		if err := nodes[i].EstablishP2PConns(); err != nil {
			panic(err)
		}
		nodes[i].EstablishRPCConns()
		nodes[i].EstablishClientRPC()
		// nodes[i].EstablishClientRPC()
		/*
			if err := nodes[i].EstablishP2PSyncConns(); err != nil {
				panic(err)
			}
		*/
	}
	return nodes
}

func Test4Nodes(t *testing.T) {
	nodes := setupNodes()
	for i := 0; i < 4; i++ {
		fmt.Printf("node%d starts the protocol!\n", i)
		//go nodes[i].startRPC()
		go nodes[i].HandleMsgsLoop()
		// go nodes[i].HandleHSMsgsLoop()
	}

	for i := 0; i < 4; i++ {
		go nodes[i].Bolt.ProposalLoop(0)
		go nodes[i].BroadcastClientPayLoadLoop()
		go nodes[i].BroadcastPayLoadLoop()
	}

	for {
		time.Sleep(time.Second)
	}

}
