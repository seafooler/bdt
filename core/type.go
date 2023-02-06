package core

import "reflect"

// Message tags to indicate the type of message.
const (
	BoltProposalMsgTag uint8 = iota
	BoltVoteMsgTag
)

var boltProposalMsg BoltProposalMsg
var boltVoteMsg BoltVoteMsg

var reflectedTypesMap = map[uint8]reflect.Type{
	BoltProposalMsgTag: reflect.TypeOf(boltProposalMsg),
	BoltVoteMsgTag:     reflect.TypeOf(boltVoteMsg),
}

type Block struct {
	Reqs     []byte
	Height   uint32
	Proposer int
}

type BoltProposalMsg struct {
	Block
	Proof []byte
}

type BoltVoteMsg struct {
	Share  []byte
	Height uint32
	Voter  int
}

type ProofData struct {
	Proof  []byte
	Height uint32
}
