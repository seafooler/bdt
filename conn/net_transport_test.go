package conn

import (
	"reflect"
	"testing"
	"time"
)

const (
	PBValTag uint8 = iota
	PBVoteTag
)

var pbMsgType = map[string]uint8{
	"VAL": PBValTag,
	"PBV": PBVoteTag,
}

var pbValMsg VALMessage
var pbVoteMsg VOTMessage
var reflectedTypesMap = map[uint8]reflect.Type{
	PBValTag:  reflect.TypeOf(pbValMsg),
	PBVoteTag: reflect.TypeOf(pbVoteMsg),
}

type SNView struct {
	SN   int
	View int
}

type SNViewPhase struct {
	SNView
	Phase uint8 // phase can only be 0 or 1
}

type VALMessage struct {
	RawData []byte
	Proof   []byte
	Dealer  string // dealer and sender are the same
	SNViewPhase
}

type VOTMessage struct {
	PartialSig []byte
	Dealer     string
	Sender     string
	SNViewPhase
}

var typesMap map[uint8]string

// TestSimpleComm tests if node1 (addr1, client) can connect to node2 (addr2, server) correctly
// And if node1 can send message of type 'MsgType1' to node2
// And if node2 can respond with the message of type 'MsgType1Resp' to node1
func TestSimpleComm(t *testing.T) {
	var val VALMessage
	var vot VOTMessage
	var reflectedTypesMap = map[uint8]reflect.Type{
		PBValTag:  reflect.TypeOf(val),
		PBVoteTag: reflect.TypeOf(vot),
	}

	valMsg := VALMessage{
		RawData: []byte("seafooler"),
		Proof:   nil,
		Dealer:  "seafooler",
		SNViewPhase: SNViewPhase{
			SNView: SNView{0, 0},
			Phase:  0,
		},
	}

	addr1 := "127.0.0.1:8888"
	tran1, _ := NewTCPTransport(addr1, 2*time.Second, nil, 1, reflectedTypesMap)
	tran1.reflectedTypesMap = reflectedTypesMap
	defer tran1.Close()

	// Listen for a request
	go func() {
		msgWithSig := <-tran1.msgCh
		receivedPerson, ok := msgWithSig.(VALMessage)
		if !ok {
			t.Fatal("received msg is not of type: VALMessage")
		}
		if receivedPerson.SN != valMsg.SN || receivedPerson.Dealer != valMsg.Dealer {
			t.Fatal("received VALMessage does not match the original one")
		}
	}()

	addr2 := "127.0.0.1:9999"
	tran2, _ := NewTCPTransport(addr2, 2*time.Second, nil, 1, reflectedTypesMap)
	tran2.reflectedTypesMap = reflectedTypesMap
	defer tran2.Close()

	conn, err := tran2.GetConn(addr1)
	if err != nil {
		t.Errorf(err.Error())
	}

	if err := SendMsg(conn, PBValTag, &valMsg, nil); err != nil {
		t.Errorf(err.Error())
	}

	time.Sleep(time.Second)
}
