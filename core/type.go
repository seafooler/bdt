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
	SMVBAPBValTag
	SMVBAPBVoteTag
	SMVBAFinishTag
	SMVBADoneShareTag
	SMVBAPreVoteTag
	SMVBAVoteTag
	SMVBAHaltTag
)

var msgTagNameMap = map[uint8]string{
	SMVBAPBValTag:     "VAL",
	SMVBAPBVoteTag:    "PBV",
	SMVBAFinishTag:    "FSH",
	SMVBADoneShareTag: "DOS",
	SMVBAPreVoteTag:   "PVT",
	SMVBAVoteTag:      "VOT",
}

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

type SMVBASNView struct {
	SN   int
	View int
}

type SMVBASNViewPhase struct {
	SMVBASNView
	Phase uint8 // phase can only be 1 or 2
}

type SMVBAPBVALMessage struct {
	Data   []byte
	Proof  []byte
	Dealer string // dealer and sender are the same
	SMVBASNViewPhase
}

type SMVBAPBVOTMessage struct {
	Data       []byte
	PartialSig []byte
	Dealer     string
	Sender     string
	SMVBASNViewPhase
}

type SMVBAQCedData struct {
	Data []byte
	QC   []byte
	SMVBASNViewPhase
}

type SMVBAFinishMessage struct {
	Data   []byte
	QC     []byte
	Dealer string
	SMVBASNView
}

type SMVBADoneShareMessage struct {
	TSShare []byte
	Sender  string
	SMVBASNView
}

// SMVBAPreVoteMessage must contain SNView
type SMVBAPreVoteMessage struct {
	Flag              bool
	Dealer            string
	Value             []byte
	ProofOrPartialSig []byte // lock proof (sigma_1) or rho_{pn}
	Sender            string

	SMVBASNView
}

// SMVBAVoteMessage must contain SNView
type SMVBAVoteMessage struct {
	Flag   bool
	Dealer string
	Value  []byte
	Proof  []byte // sigma_1 or sigma_{PN}
	Pho    []byte // pho_{2,i} or pho_{vn, i}

	Sender string

	SMVBASNView
}

type SMVBAHaltMessage struct {
	Value  []byte
	Proof  []byte
	Dealer string
	SMVBASNView
}

type SMVBAReadyViewData struct {
	usePrevData bool // indicate if using the previous data
	data        []byte
	proof       []byte
}

var bpMsg BoltProposalMsg
var bvMsg BoltVoteMsg
var ababrMsg ABABvalRequestMsg
var abaarMsg ABAAuxRequestMsg
var abaexMsg ABAExitMsg
var psMsg PaceSyncMsg
var pbValMsg SMVBAPBVALMessage
var pbVoteMsg SMVBAPBVOTMessage
var finishMsg SMVBAFinishMessage
var doneShareMsg SMVBADoneShareMessage
var preVoteMsg SMVBAPreVoteMessage
var voteMsg SMVBAVoteMessage
var haltMsg SMVBAHaltMessage

var reflectedTypesMap = map[uint8]reflect.Type{
	BoltProposalMsgTag:   reflect.TypeOf(bpMsg),
	BoltVoteMsgTag:       reflect.TypeOf(bvMsg),
	ABABvalRequestMsgTag: reflect.TypeOf(ababrMsg),
	ABAAuxRequestMsgTag:  reflect.TypeOf(abaarMsg),
	ABAExitMsgTag:        reflect.TypeOf(abaexMsg),
	PaceSyncMsgTag:       reflect.TypeOf(psMsg),
	SMVBAPBValTag:        reflect.TypeOf(pbValMsg),
	SMVBAPBVoteTag:       reflect.TypeOf(pbVoteMsg),
	SMVBAFinishTag:       reflect.TypeOf(finishMsg),
	SMVBADoneShareTag:    reflect.TypeOf(doneShareMsg),
	SMVBAPreVoteTag:      reflect.TypeOf(preVoteMsg),
	SMVBAVoteTag:         reflect.TypeOf(voteMsg),
	SMVBAHaltTag:         reflect.TypeOf(haltMsg),
}
