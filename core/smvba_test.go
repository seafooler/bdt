package core

import (
	"fmt"
	"testing"
	"time"
)

func TestSimpleSMVBA(t *testing.T) {
	numNode := 4
	nodes := Setup(numNode, 2, 9016, 3)

	originalDatas := make([][]byte, numNode)
	proofs := make([][]byte, numNode)

	for i, _ := range nodes {
		originalDatas[i] = []byte("seafooler" + fmt.Sprintf("%d%d%d%d", i, i, i, i))
		proofs[i] = nil
	}

	for i, node := range nodes {
		if i == 3 {
			time.Sleep(time.Millisecond * 100)
		}
		go node.Smvba.RunOneMVBAView(false, originalDatas[i], proofs[i], -1)
	}

	go func() {
		for {
			select {
			case output := <-nodes[0].Smvba.OutputCh():
				fmt.Printf("Output from node0, snv: %v, value: %s\n", output, nodes[0].Smvba.Output())
			case output := <-nodes[1].Smvba.OutputCh():
				fmt.Printf("Output from node1, snv: %v, value: %s\n", output, nodes[1].Smvba.Output())
			case output := <-nodes[2].Smvba.OutputCh():
				fmt.Printf("Output from node2, snv: %v, value: %s\n", output, nodes[2].Smvba.Output())
			case output := <-nodes[3].Smvba.OutputCh():
				fmt.Printf("Output from node3, snv: %v, value: %s\n", output, nodes[3].Smvba.Output())
			}
		}
	}()

	time.Sleep(time.Second * 6)
}
