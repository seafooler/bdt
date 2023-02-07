package core

import (
	"encoding/binary"
	"fmt"
	"github.com/hashicorp/go-hclog"
	"github.com/seafooler/sign_tools"
	"sync"
)

// ABA is the Binary Byzantine Agreement build from a common coin protocol.
type ABA struct {
	node *Node

	aLogger hclog.Logger

	// Current epoch
	epoch uint32

	// Bval requests we accepted this epoch.
	binValues map[bool]struct{}

	// sentBvals are the binary values this instance sent.
	sentBvals []bool

	// recvTrueBval is a mapping of the sender and the received binary value.
	recvTrueBval map[int]bool

	// recvFalseBval is a mapping of the sender and the received binary value.
	recvFalseBval map[int]bool

	// recvAux is a mapping of the sender and the received Aux value.
	recvAux map[int]bool

	// recvParSig maintains the received partial signatures to reveal the coin
	recvParSig [][]byte

	// recvAux is a mapping of the sender and the received exitMessage value.
	exitMsgs map[int]bool

	// hasSentExitMsg indicates if the replica has sent the ExitMsg
	hasSentExitMsg bool

	// indicate if aba is finished and exited
	done  bool
	print bool

	// output and estimated of the aba protocol. This can be either nil or a
	// boolean.
	output, estimated interface{}

	//cachedBvalMsgs and cachedAuxMsgs cache messages that are received by a node that is
	// in a later epoch.
	cachedBvalMsgs map[uint32][]*ABABvalRequestMsg
	cachedAuxMsgs  map[uint32][]*ABAAuxRequestMsg

	lock sync.RWMutex
}

// NewABA returns a new instance of the Binary Byzantine Agreement.
func NewABA(node *Node) *ABA {
	return &ABA{
		node: node,
		aLogger: hclog.New(&hclog.LoggerOptions{
			Name:   "bdt-aba",
			Output: hclog.DefaultOutput,
			Level:  hclog.Level(node.Config.LogLevel),
		}),
		epoch:          0,
		recvTrueBval:   make(map[int]bool),
		recvFalseBval:  make(map[int]bool),
		recvAux:        make(map[int]bool),
		exitMsgs:       make(map[int]bool),
		sentBvals:      []bool{},
		binValues:      make(map[bool]struct{}),
		cachedBvalMsgs: make(map[uint32][]*ABABvalRequestMsg),
		cachedAuxMsgs:  make(map[uint32][]*ABAAuxRequestMsg),
	}
}

// inputValue will set the given val as the initial value to be proposed in the
// Agreement.
func (b *ABA) inputValue(val bool) error {
	// Make sure we are in the first epoch round.
	if b.epoch != 0 || b.estimated != nil {
		return nil
	}
	b.estimated = val
	b.sentBvals = append(b.sentBvals, val)
	msg := ABABvalRequestMsg{
		Sender: b.node.Id,
		Epoch:  b.epoch,
		Value:  val,
	}
	return b.node.PlainBroadcast(ABABvalRequestMsgTag, msg, nil)
}

// handleBvalRequest processes the received binary value and fills up the
// message queue if there are any messages that need to be broadcast.
func (b *ABA) handleBvalRequest(msg *ABABvalRequestMsg) error {
	b.lock.Lock()
	defer b.lock.Unlock()

	b.aLogger.Debug("receive an bval msg", "replica", b.node.Id, "cur_epoch", b.epoch,
		"msg.Epoch", msg.Epoch, "msg.Value", msg.Value, "msg.Sender", msg.Sender, "trueBval", b.recvTrueBval,
		"falseBval", b.recvFalseBval, "aux", b.recvAux, "binValues", b.binValues)

	if b.done {
		if !b.print {
			b.aLogger.Info("~~~~~~~~~~~~~~~~ABA is finished~~~~~~~~~~~~~~~~~~", "replica", b.node.Id)
			b.print = true
		}
		return nil
	}

	// Ignore messages from older epochs.
	if msg.Epoch < b.epoch {
		b.aLogger.Debug("receive a bval msg from an older epoch", "replica", b.node.Id, "cur_epoch", b.epoch,
			"msg.Epoch", msg.Epoch)
		return nil
	}
	// Messages from later epochs will be qued and processed later.
	if msg.Epoch > b.epoch {
		b.aLogger.Debug("receive a bval msg from a future epoch", "replica", b.node.Id, "cur_epoch", b.epoch,
			"msg.Epoch", msg.Epoch)
		b.cachedBvalMsgs[b.epoch] = append(b.cachedBvalMsgs[b.epoch], msg)
		return nil
	}

	if msg.Value {
		b.recvTrueBval[msg.Sender] = msg.Value
	} else {
		b.recvFalseBval[msg.Sender] = msg.Value
	}
	lenBval := b.countBvals(msg.Value)

	// When receiving input(b) messages from f + 1 nodes, if inputs(b) is not
	// been sent yet broadcast input(b) and handle the input ourselves.
	if lenBval == b.node.F+1 && !b.hasSentBval(msg.Value) {
		b.sentBvals = append(b.sentBvals, msg.Value)
		m := ABABvalRequestMsg{
			Sender: b.node.Id,
			Epoch:  b.epoch,
			Value:  msg.Value,
		}
		if err := b.node.PlainBroadcast(ABABvalRequestMsgTag, m, nil); err != nil {
			return err
		}
	}

	// When receiving n bval(b) messages from 2f+1 nodes: inputs := inputs u {b}
	if lenBval == 2*b.node.F+1 {
		wasEmptyBinValues := len(b.binValues) == 0
		b.binValues[msg.Value] = struct{}{}
		// If inputs > 0 broadcast output(b) and handle the output ourselfs.
		// Wait until binValues > 0, then broadcast AUX(b). The AUX(b) broadcast
		// may only occure once per epoch.
		if wasEmptyBinValues {
			parSig := sign_tools.SignTSPartial(b.node.PriKeyTS, []byte(fmt.Sprint(b.epoch)))
			m := ABAAuxRequestMsg{
				Sender: b.node.Id,
				Epoch:  b.epoch,
				Value:  msg.Value,
				TSPar:  parSig,
			}
			if err := b.node.PlainBroadcast(ABAAuxRequestMsgTag, m, nil); err != nil {
				return err
			}
		}
		b.tryOutputAgreement()
	}
	return nil
}

func (b *ABA) handleAuxRequest(msg *ABAAuxRequestMsg) error {
	b.lock.Lock()
	defer b.lock.Unlock()

	b.aLogger.Debug("receive an aux msg", "replica", b.node.Id, "cur_epoch", b.epoch,
		"msg.Epoch", msg.Epoch, "msg.Value", msg.Value, "msg.Sender", msg.Sender, "trueBval", b.recvTrueBval,
		"falseBval", b.recvFalseBval, "aux", b.recvAux, "binValues", b.binValues)

	if b.done {
		if !b.print {
			b.aLogger.Info("~~~~~~~~~~~~~~~~ABA is finished~~~~~~~~~~~~~~~~~~", "replica", b.node.Id)
			b.print = true
		}
		return nil
	}

	// Ignore messages from older epochs.
	if msg.Epoch < b.epoch {
		b.aLogger.Debug("receive an aux msg from an older epoch", "replica", b.node.Id, "cur_epoch", b.epoch,
			"msg.Epoch", msg.Epoch)
		return nil
	}
	// Messages from later epochs will be qued and processed later.
	if msg.Epoch > b.epoch {
		b.aLogger.Debug("receive an aux msg from a future epoch", "replica", b.node.Id, "cur_epoch", b.epoch,
			"msg.Epoch", msg.Epoch)
		b.cachedAuxMsgs[b.epoch] = append(b.cachedAuxMsgs[b.epoch], msg)
		return nil
	}

	b.recvAux[msg.Sender] = msg.Value
	b.recvParSig = append(b.recvParSig, msg.TSPar)
	b.tryOutputAgreement()
	return nil
}

// tryOutputAgreement waits until at least (N - f) output messages received,
// once the (N - f) messages are received, make a common coin and uses it to
// compute the next decision estimate and output the optional decision value.
func (b *ABA) tryOutputAgreement() {
	if len(b.binValues) == 0 {
		return
	}
	// Wait longer till eventually receive (N - F) aux messages.
	lenOutputs, values := b.countAuxs()
	if lenOutputs < b.node.N-b.node.F {
		return
	}

	// figure out the coin
	parSigs := b.recvParSig[:b.node.N-b.node.F]
	intactSig := sign_tools.AssembleIntactTSPartial(parSigs, b.node.PubKeyTS,
		[]byte(fmt.Sprint(b.epoch)), b.node.N-b.node.F, b.node.N)
	data := binary.BigEndian.Uint64(intactSig)
	coin := true
	if data%2 == 1 {
		coin = false
	}
	b.aLogger.Debug("assemble the data and reveal the coin", "replica", b.node.Id, "epoch", b.epoch,
		"data", data, "coin", coin)

	if len(values) != 1 {
		b.estimated = coin
	} else {
		b.estimated = values[0]
		// Output may be set only once.
		if b.output == nil && values[0] == coin {
			b.output = values[0]
			b.aLogger.Info("output the agreed value", "replica", b.node.Id, "value", values[0],
				"epoch", b.epoch)
			b.hasSentExitMsg = true
			msg := ABAExitMsg{
				Sender: b.node.Id,
				Value:  values[0],
			}
			if err := b.node.PlainBroadcast(ABAExitMsgTag, msg, nil); err != nil {
				b.aLogger.Error(err.Error())
			}
		}
	}

	if b.done {
		if !b.print {
			b.aLogger.Info("~~~~~~~~~~~~~~~~ABA is finished~~~~~~~~~~~~~~~~~~", "replica", b.node.Id)
			b.print = true
		}
		return
	}

	// Start the next epoch.
	b.aLogger.Debug("advancing to the next epoch after receiving aux messages", "replica", b.node.Id,
		"next_epoch", b.epoch+1, "aux_msg_count", lenOutputs)
	b.advanceEpoch()

	estimated := b.estimated.(bool)
	b.sentBvals = append(b.sentBvals, estimated)

	msg := ABABvalRequestMsg{
		Sender: b.node.Id,
		Epoch:  b.epoch,
		Value:  estimated,
	}
	if err := b.node.PlainBroadcast(ABABvalRequestMsgTag, msg, nil); err != nil {
		b.aLogger.Error(err.Error(), "replica", b.node.Id)
	}

	// process the cached messages for the next epoch.
	if cachedBvalMsgs, ok := b.cachedBvalMsgs[b.epoch]; ok {
		for _, cm := range cachedBvalMsgs {
			go func(m *ABABvalRequestMsg) {
				b.handleBvalRequest(m)
			}(cm)
		}
	}
	delete(b.cachedBvalMsgs, b.epoch)

	if cachedAuxMsgs, ok := b.cachedAuxMsgs[b.epoch]; ok {
		for _, cm := range cachedAuxMsgs {
			go func(m *ABAAuxRequestMsg) {
				b.handleAuxRequest(m)
			}(cm)
		}
	}
	delete(b.cachedAuxMsgs, b.epoch)
}

// countBvals counts all the received Bval inputs matching b.
// this function must be called in a mutex-protected env
func (b *ABA) countBvals(ok bool) int {
	var toCheckBval map[int]bool
	if ok {
		toCheckBval = b.recvTrueBval
	} else {
		toCheckBval = b.recvFalseBval
	}

	n := 0
	for _, val := range toCheckBval {
		if val == ok {
			n++
		}
	}
	return n
}

// hasSentBval return true if we already sent out the given value.
func (b *ABA) hasSentBval(val bool) bool {
	for _, ok := range b.sentBvals {
		if ok == val {
			return true
		}
	}
	return false
}

// advanceEpoch will reset all the values that are bound to an epoch and increments
// the epoch value by 1.
func (b *ABA) advanceEpoch() {
	b.binValues = make(map[bool]struct{})
	b.sentBvals = []bool{}
	b.recvAux = make(map[int]bool)
	b.recvTrueBval = make(map[int]bool)
	b.recvFalseBval = make(map[int]bool)
	b.recvParSig = [][]byte{}
	b.epoch++
}

func (b *ABA) handleExitMessage(msg *ABAExitMsg) error {
	b.lock.Lock()
	defer b.lock.Unlock()

	if b.done {
		if !b.print {
			b.aLogger.Info("~~~~~~~~~~~~~~~~ABA is finished~~~~~~~~~~~~~~~~~~", "replica", b.node.Id)
			b.print = true
		}
		return nil
	}

	b.exitMsgs[msg.Sender] = msg.Value
	lenEM := b.countExitMessages(msg.Value)
	if lenEM == b.node.F+1 && !b.hasSentExitMsg {
		b.hasSentExitMsg = true
		m := ABAExitMsg{
			Sender: b.node.Id,
			Value:  msg.Value,
		}
		if err := b.node.PlainBroadcast(ABAExitMsgTag, m, nil); err != nil {
			b.aLogger.Error(err.Error(), "replica", b.node.Id)
		}
	}

	if lenEM == 2*b.node.F+1 {
		if b.output == nil {
			b.aLogger.Info("output the agreed value after receiving 2f+1 exit msgs", "replica", b.node.Id,
				"value", msg.Value, "epoch", b.epoch)
			b.output = msg.Value
		}
		b.done = true
	}
	return nil
}

// countExitMessages counts all the exitMessages matching v.
// this function must be called in a mutex-protected env
func (b *ABA) countExitMessages(v bool) int {
	n := 0
	for _, val := range b.exitMsgs {
		if val == v {
			n++
		}
	}
	return n
}

// countAuxs returns the number of received (aux) messages, the corresponding
// values that where also in our inputs.
func (b *ABA) countAuxs() (int, []bool) {
	valsMap := make(map[bool]struct{})
	numQualifiedAux := 0
	for _, val := range b.recvAux {
		if _, ok := b.binValues[val]; ok {
			valsMap[val] = struct{}{}
			numQualifiedAux++
		}
	}

	var valsSet []bool
	for val, _ := range valsMap {
		valsSet = append(valsSet, val)
	}

	return numQualifiedAux, valsSet
}
