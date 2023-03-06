package main

import (
	"github.com/seafooler/bdt/config"
	"github.com/seafooler/bdt/core"
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
	if err = node.StartP2PListen(); err != nil {
		panic(err)
	}

	// wait for each node to start
	time.Sleep(time.Second * time.Duration(conf.WaitTime))

	if err = node.EstablishP2PConns(); err != nil {
		panic(err)
	}

	go node.HandleMsgsLoop()

	go node.Bolt.ProposalLoop(0)

	for {
		time.Sleep(time.Second)
	}

}
