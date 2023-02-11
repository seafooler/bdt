package core

import (
	"bytes"
	"testing"
)

func TestSimpleSinglePB(t *testing.T) {
	nodes := Setup(4, 2, 9006, 3)

	originalData := []byte("seafooler")

	if err := nodes[0].Smvba.spb.pb1.PBBroadcastData(originalData, nil, 1, 1); err != nil {
		t.Fatal(err)
	}

	data := <-nodes[0].Smvba.spb.pb1.pbOutputCh

	if !bytes.Equal(originalData, data.Data) {
		t.Fatalf("The QCed data does not equal the original one, original: %s, qced: %s",
			originalData, data.Data[:len(data.Data)-1])
	}

	if ok, err := nodes[0].Smvba.VerifyTS(data.Data, data.QC); !ok {
		t.Fatal(err)
	}
}
