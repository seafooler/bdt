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
	SN       int
	TxNum    int
	Reqs     []byte
	Height   int
	Proposer int
}

type BoltProposalMsg struct {
	Block
	Proof []byte
}

type BoltVoteMsg struct {
	SN     int
	Share  []byte
	Height int
	Voter  int
}

type ProofData struct {
	SN     int
	Proof  []byte
	Height int
}

// ABABvalRequestMsg holds the input value of the binary input.
type ABABvalRequestMsg struct {
	SN     int
	Sender int
	Round  uint32
	Value  int
}

// ABAAuxRequestMsg holds the output value.
type ABAAuxRequestMsg struct {
	SN     int
	Sender int
	Round  uint32
	Value  int
	TSPar  []byte
}

// ABAExitMsg indicates that a replica has decided
type ABAExitMsg struct {
	SN     int
	Sender int
	Value  int
}

// PaceSyncMsg
type PaceSyncMsg struct {
	SN     int
	Sender int
	Epoch  int
	Proof  []byte
}

type SMVBAViewPhase struct {
	View  int
	Phase uint8 // phase can only be 1 or 2
}

type SMVBAPBVALMessage struct {
	SN      int
	TxCount int
	Data    []byte
	Proof   []byte
	Dealer  string // dealer and sender are the same
	SMVBAViewPhase
}

type SMVBAPBVOTMessage struct {
	SN         int
	TxCount    int
	Hash       []byte
	PartialSig []byte
	Dealer     string
	Sender     string
	SMVBAViewPhase
}

type SMVBAQCedData struct {
	SN      int
	TxCount int
	Hash    []byte
	QC      []byte
	SMVBAViewPhase
}

type SMVBAFinishMessage struct {
	SN      int
	Hash    []byte
	QC      []byte
	Dealer  string
	TxCount int
	View    int
}

type SMVBADoneShareMessage struct {
	SN      int
	TSShare []byte
	Sender  string
	TxCount int
	View    int
}

// SMVBAPreVoteMessage must contain SNView
type SMVBAPreVoteMessage struct {
	SN                int
	TxCount           int
	Flag              bool
	Dealer            string
	Hash              []byte
	ProofOrPartialSig []byte // lock proof (sigma_1) or rho_{pn}
	Sender            string

	View int
}

// SMVBAVoteMessage must contain SNView
type SMVBAVoteMessage struct {
	SN      int
	TxCount int
	Flag    bool
	Dealer  string
	Hash    []byte
	Proof   []byte // sigma_1 or sigma_{PN}
	Pho     []byte // pho_{2,i} or pho_{vn, i}

	Sender string

	View int
}

type SMVBAHaltMessage struct {
	SN      int
	TxCount int
	Hash    []byte
	Proof   []byte
	Dealer  string
	View    int
}

type SMVBAReadyViewData struct {
	SN          int
	txCount     int
	usePrevData bool // indicate if using the previous data
	data        []byte
	proof       []byte
}

type StatusChangeSignal struct {
	SN     int
	Status uint8
}

var bpMsg BoltProposalMsg
var bvMsg BoltVoteMsg
var ababrMsg ABABvalRequestMsg
var abaarMsg ABAAuxRequestMsg
var abaexMsg ABAExitMsg
var psMsg PaceSyncMsg
var smvbaPbValMsg SMVBAPBVALMessage
var smvbaPbVoteMsg SMVBAPBVOTMessage
var smvbaFinishMsg SMVBAFinishMessage
var smvbaDoneShareMsg SMVBADoneShareMessage
var smvbaPreVoteMsg SMVBAPreVoteMessage
var smvbaVoteMsg SMVBAVoteMessage
var smvbaHaltMsg SMVBAHaltMessage

var reflectedTypesMap = map[uint8]reflect.Type{
	BoltProposalMsgTag:   reflect.TypeOf(bpMsg),
	BoltVoteMsgTag:       reflect.TypeOf(bvMsg),
	ABABvalRequestMsgTag: reflect.TypeOf(ababrMsg),
	ABAAuxRequestMsgTag:  reflect.TypeOf(abaarMsg),
	ABAExitMsgTag:        reflect.TypeOf(abaexMsg),
	PaceSyncMsgTag:       reflect.TypeOf(psMsg),
	SMVBAPBValTag:        reflect.TypeOf(smvbaPbValMsg),
	SMVBAPBVoteTag:       reflect.TypeOf(smvbaPbVoteMsg),
	SMVBAFinishTag:       reflect.TypeOf(smvbaFinishMsg),
	SMVBADoneShareTag:    reflect.TypeOf(smvbaDoneShareMsg),
	SMVBAPreVoteTag:      reflect.TypeOf(smvbaPreVoteMsg),
	SMVBAVoteTag:         reflect.TypeOf(smvbaVoteMsg),
	SMVBAHaltTag:         reflect.TypeOf(smvbaHaltMsg),
}
