package core

import (
	"crypto/ed25519"
	"github.com/seafooler/bdt/config"
	"github.com/seafooler/sign_tools"
	"strconv"
	"time"
)

func Setup(numNode int, stat uint8, startPort, payLoadStartPort int, logLevel int) []*Node {
	id2NameMap := make(map[int]string, numNode)
	name2IdMap := make(map[string]int, numNode)
	id2AddrMap := make(map[int]string, numNode)
	id2PortMap := make(map[int]string, numNode)
	id2PortPayloadMap := make(map[int]string, numNode)
	for i := 0; i < numNode; i++ {
		name := "node" + strconv.Itoa(i)
		addr := "127.0.0.1"
		port := strconv.Itoa(startPort + i)
		id2NameMap[i] = name
		name2IdMap[name] = i
		id2AddrMap[i] = addr
		id2PortMap[i] = port
		payLoadPort := strconv.Itoa(payLoadStartPort + i)
		id2PortPayloadMap[i] = payLoadPort
	}

	shares, pubKey := sign_tools.GenTSKeys(numNode/3*2+1, numNode)

	nodes := make([]*Node, numNode)

	sks := make([]ed25519.PrivateKey, numNode)
	pksMap := make(map[int]ed25519.PublicKey)
	for i := 0; i < numNode; i++ {
		sk, pk := sign_tools.GenED25519Keys()
		sks[i] = sk
		pksMap[i] = pk
	}

	for id, name := range id2NameMap {
		conf := config.New(id, name, id2NameMap, name2IdMap, id2AddrMap[id], id2PortMap[id], id2PortPayloadMap[id],
			shares[id], pubKey, sks[id], pksMap, id2AddrMap, id2PortMap, id2PortPayloadMap, 10, logLevel, 500, 0, false,
			1000, 500, 512, 1000, 512, 20)

		nodes[id] = NewNode(conf)
		nodes[id].status = stat
	}

	for _, node := range nodes {
		if err := node.StartP2PListen(); err != nil {
			panic(err)
		}
	}

	for _, node := range nodes {
		go node.EstablishP2PConns()
	}

	//Wait the all the connections to be established
	time.Sleep(time.Second)

	for _, node := range nodes {
		go node.HandleMsgsLoop()
	}

	return nodes
}
