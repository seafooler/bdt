package core

import "reflect"

// Message tags to indicate the type of message.
const (
	BoltProposalMsgTag uint8 = iota
	BoltVoteMsgTag
	ABABvalRequestMsgTag
	ABAExitMsgTag
	ABAAuxRequestMsgTag
	PaceSyncMsgTag
)

type Block struct {
	Reqs     []byte
	Height   int
	Proposer int
}

type BoltProposalMsg struct {
	Block
	Proof []byte
}

type BoltVoteMsg struct {
	Share  []byte
	Height int
	Voter  int
}

type ProofData struct {
	Proof  []byte
	Height int
}

// ABABvalRequestMsg holds the input value of the binary input.
type ABABvalRequestMsg struct {
	Sender int
	Round  uint32
	Value  int
}

// ABAAuxRequestMsg holds the output value.
type ABAAuxRequestMsg struct {
	Sender int
	Round  uint32
	Value  int
	TSPar  []byte
}

// ABAExitMsg indicates that a replica has decided
type ABAExitMsg struct {
	Sender int
	Value  int
}

// PaceSyncMsg
type PaceSyncMsg struct {
	Sender int
	Epoch  int
	Proof  []byte
}

var bpMsg BoltProposalMsg
var bvMsg BoltVoteMsg
var ababrMsg ABABvalRequestMsg
var abaarMsg ABAAuxRequestMsg
var abaexMsg ABAExitMsg
var psMsg PaceSyncMsg

var reflectedTypesMap = map[uint8]reflect.Type{
	BoltProposalMsgTag:   reflect.TypeOf(bpMsg),
	BoltVoteMsgTag:       reflect.TypeOf(bvMsg),
	ABABvalRequestMsgTag: reflect.TypeOf(ababrMsg),
	ABAAuxRequestMsgTag:  reflect.TypeOf(abaarMsg),
	ABAExitMsgTag:        reflect.TypeOf(abaexMsg),
	PaceSyncMsgTag:       reflect.TypeOf(psMsg),
}
