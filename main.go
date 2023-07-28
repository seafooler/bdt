package main

import (
	"github.com/seafooler/bdt/config"
	"github.com/seafooler/bdt/core"
	"strconv"
	"time"
)

var conf *config.Config
var err error

func init() {
	conf, err = config.LoadConfig("", "config")
	if err != nil {
		panic(err)
	}
}

func main() {
	//logger := hclog.New(&hclog.LoggerOptions{
	//	Name:   "bdt-main",
	//	Output: hclog.DefaultOutput,
	//	Level:  hclog.Level(conf.LogLevel),
	//})

	node := core.NewNode(conf)
	node.ClientPort = "9000"
	node.Client.Name = "Client" + strconv.Itoa(node.Id)

	if err = node.StartP2PListen(); err != nil {
		panic(err)
	}
	go node.StartListenRPC()
	go node.StartClientRPC()

	// wait for each node to start
	time.Sleep(time.Second * time.Duration(conf.WaitTime))

	if err = node.EstablishP2PConns(); err != nil {
		panic(err)
	}

	node.EstablishClientRPC()
	node.EstablishRPCConns()

	// Help all the replicas to start simultaneously
	node.BroadcastSyncLaunchMsgs()
	node.WaitForEnoughSyncLaunchMsgs()

	go node.HandleMsgsLoop()
	//go node.HandlePayLoadMsgsLoop()

	go node.Bolt.ProposalLoop(0)
	go node.BroadcastClientPayLoadLoop()
	node.BroadcastPayLoadLoop()

}
