package core

import "reflect"

// Message tags to indicate the type of message.
const (
	BoltProposalMsgTag uint8 = iota
	BoltVoteMsgTag
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

var bpMsg BoltProposalMsg
var bvMsg BoltVoteMsg

var reflectedTypesMap = map[uint8]reflect.Type{
	BoltProposalMsgTag: reflect.TypeOf(bpMsg),
	BoltVoteMsgTag:     reflect.TypeOf(bvMsg),
}
