package core

import (
	"bytes"
	"fmt"
	"testing"
)

// Only replica 0 issues an SPB
func TestSimpleSPB(t *testing.T) {
	nodes := Setup(4, 2, 3)

	originalData := []byte("seafooler")

	var qcedDataCh chan SMVBAQCedData
	var err error
	if qcedDataCh, err = nodes[0].Smvba.BroadcastViaSPB(originalData, nil, 1, 0); err != nil {
		t.Fatal(err)
	}

	qcedData := <-qcedDataCh
	data := qcedData.Data

	if !bytes.Equal(originalData, data) {
		t.Fatal("The QCed data does not equal the original one")
	}

	if ok, err := nodes[0].Smvba.VerifyTS(data, qcedData.QC); !ok {
		t.Fatal(err)
	}
}

// Each replica issues an SPB
func TestParallelSPB(t *testing.T) {
	numNode := 4
	nodes := Setup(numNode, 2, 3)

	// broadcast data via spb
	qcedDataChs := make([]chan SMVBAQCedData, numNode)
	var err error
	originalData := make([][]byte, numNode)

	for i, node := range nodes {
		originalData[i] = []byte("seafooler" + fmt.Sprintf("%d%d%d%d", i, i, i, i))
		if qcedDataChs[i], err = node.Smvba.BroadcastViaSPB(originalData[i], nil, 1, 0); err != nil {
			t.Fatal(err)
		}
	}

	// wait for spbs to finish
	type QCedDataWithIndex struct {
		SMVBAQCedData
		i int
	}
	qdIndexCh := make(chan QCedDataWithIndex)

	for i, ch := range qcedDataChs {
		go func(c chan SMVBAQCedData, i int) {
			data := <-c
			qdIndexCh <- QCedDataWithIndex{SMVBAQCedData: data, i: i}
		}(ch, i)
	}

	for i := 0; i < numNode; i++ {
		var dataIndex QCedDataWithIndex

		dataIndex = <-qdIndexCh
		k := dataIndex.i
		qcedData := dataIndex.SMVBAQCedData
		data := qcedData.Data

		if !bytes.Equal(originalData[k], data) {
			t.Fatal("The QCed data does not equal the original one")
		}

		if ok, err := nodes[0].Smvba.VerifyTS(data, qcedData.QC); !ok {
			t.Fatal(err)
		}

	}
}
