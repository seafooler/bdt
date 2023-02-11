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

	// Current round
	round uint32

	// Bval requests we accepted this round.
	binValues map[int]struct{}

	// sentBvals are the binary values this instance sent
	// historical sentBvals must also be maintained due to the asynchronous network
	sentBvals map[uint32][2]bool

	// recvOddBval is a mapping of the sender and the received odd value
	// historical Bvals must also be maintained due to the asynchronous network
	recvOddBval map[uint32]map[int]bool

	// recvEvenBval is a mapping of the sender and the received even value
	// historical Bvals must also be maintained due to the asynchronous network
	recvEvenBval map[uint32]map[int]bool

	// recvAux is a mapping of the sender and the received Aux value.
	recvAux map[int]int

	// recvParSig maintains the received partial signatures to reveal the coin
	recvParSig [][]byte

	// recvAux is a mapping of the sender and the received exitMessage value.
	exitMsgs map[int]int

	// hasSentExitMsg indicates if the replica has sent the ExitMsg
	hasSentExitMsg bool

	// indicate if aba is finished and exited
	done  bool
	print bool

	// output and estimated of the aba protocol. This can be either nil or a
	// boolean.
	output, estimated interface{}

	//cachedBvalMsgs and cachedAuxMsgs cache messages that are received by a node that is
	// in a later round.
	cachedBvalMsgs map[uint32][]*ABABvalRequestMsg
	cachedAuxMsgs  map[uint32][]*ABAAuxRequestMsg

	lock sync.Mutex
}

// NewABA returns a new instance of the Binary Byzantine Agreement.
func NewABA(node *Node) *ABA {
	aBA := &ABA{
		node: node,
		aLogger: hclog.New(&hclog.LoggerOptions{
			Name:   "bdt-aba",
			Output: hclog.DefaultOutput,
			Level:  hclog.Level(node.Config.LogLevel),
		}),
		round:          0,
		recvOddBval:    make(map[uint32]map[int]bool),
		recvEvenBval:   make(map[uint32]map[int]bool),
		recvAux:        make(map[int]int),
		exitMsgs:       make(map[int]int),
		sentBvals:      make(map[uint32][2]bool),
		binValues:      make(map[int]struct{}),
		cachedBvalMsgs: make(map[uint32][]*ABABvalRequestMsg),
		cachedAuxMsgs:  make(map[uint32][]*ABAAuxRequestMsg),
	}

	aBA.recvOddBval[aBA.round] = make(map[int]bool)
	aBA.recvEvenBval[aBA.round] = make(map[int]bool)

	return aBA
}

// inputValue will set the given val as the initial value to be proposed in the
// Agreement.
func (b *ABA) inputValue(e int) error {
	b.lock.Lock()
	defer b.lock.Unlock()
	b.aLogger.Info("!!!!!!!!!!!!!!!!!!!! ABA is launched !!!!!!!!!!!!!!!!!!!!", "e", e)

	// Make sure we are in the first round.
	if b.round != 0 || b.estimated != nil {
		return nil
	}
	b.estimated = e
	if e%2 == 0 {
		// set the first value as true
		b.sentBvals[0] = [2]bool{true, false}
	} else {
		// set the second value as true
		b.sentBvals[0] = [2]bool{false, true}
	}
	msg := ABABvalRequestMsg{
		SN:     b.node.sn,
		Sender: b.node.Id,
		Round:  b.round,
		Value:  e,
	}
	return b.node.PlainBroadcast(ABABvalRequestMsgTag, msg, nil)
}

// handleBvalRequest processes the received binary value and fills up the
// message queue if there are any messages that need to be broadcast.
func (b *ABA) handleBvalRequest(msg *ABABvalRequestMsg) error {
	b.lock.Lock()
	defer b.lock.Unlock()

	b.aLogger.Debug("receive an bval msg", "replica", b.node.Id, "cur_round", b.round,
		"msg.Round", msg.Round, "msg.Value", msg.Value, "msg.Sender", msg.Sender, "oddBval", b.recvOddBval,
		"evenBval", b.recvEvenBval, "aux", b.recvAux, "binValues", b.binValues)

	// Messages from later rounds will be qued and processed later.
	if msg.Round > b.round {
		b.aLogger.Debug("receive a bval msg from a future round", "replica", b.node.Id, "cur_round", b.round,
			"msg.Round", msg.Round)
		b.cachedBvalMsgs[msg.Round] = append(b.cachedBvalMsgs[msg.Round], msg)
		return nil
	}

	// Need to update binValues and broadcast corresponding bvals even if receiving an obsolete message
	if msg.Value%2 == 1 {
		b.recvOddBval[msg.Round][msg.Sender] = true
	} else {
		b.recvEvenBval[msg.Round][msg.Sender] = true
	}
	lenBval := b.countBvals(msg.Value, msg.Round)

	// When receiving input(b) messages from f + 1 nodes, if inputs(b) is not
	// been sent yet broadcast input(b) and handle the input ourselves.
	if lenBval == b.node.F+1 && !b.hasSentBval(msg.Value, msg.Round) {
		sb := b.sentBvals[msg.Round]
		if msg.Value%2 == 0 {
			sb[0] = true
		} else {
			sb[1] = true
		}
		b.sentBvals[msg.Round] = sb
		m := ABABvalRequestMsg{
			SN:     msg.SN,
			Sender: b.node.Id,
			Round:  msg.Round,
			Value:  msg.Value,
		}
		if err := b.node.PlainBroadcast(ABABvalRequestMsgTag, m, nil); err != nil {
			return err
		}
	}

	// No need to update binValues after receiving an obsolete message
	if msg.Round < b.round {
		b.aLogger.Debug("receive a bval msg from an older round", "replica", b.node.Id, "cur_round", b.round,
			"msg.Round", msg.Round)
		return nil
	}

	// When receiving n bval(b) messages from 2f+1 nodes: inputs := inputs u {b}
	if lenBval == 2*b.node.F+1 {
		wasEmptyBinValues := len(b.binValues) == 0
		b.binValues[msg.Value] = struct{}{}
		// If inputs > 0 broadcast output(b) and handle the output ourselfs.
		// Wait until binValues > 0, then broadcast AUX(b). The AUX(b) broadcast
		// may only occur once per round.
		if wasEmptyBinValues {
			parSig := sign_tools.SignTSPartial(b.node.PriKeyTS, []byte(fmt.Sprint(b.round)))
			m := ABAAuxRequestMsg{
				SN:     msg.SN,
				Sender: b.node.Id,
				Round:  b.round,
				Value:  msg.Value,
				TSPar:  parSig,
			}
			if err := b.node.PlainBroadcast(ABAAuxRequestMsgTag, m, nil); err != nil {
				return err
			}
		}
		b.tryOutputAgreement(msg.SN)
	}
	return nil
}

func (b *ABA) handleAuxRequest(msg *ABAAuxRequestMsg) error {
	b.lock.Lock()
	defer b.lock.Unlock()

	b.aLogger.Debug("receive an aux msg", "replica", b.node.Id, "cur_round", b.round,
		"msg.Round", msg.Round, "msg.Value", msg.Value, "msg.Sender", msg.Sender, "oddBval", b.recvOddBval,
		"evenBval", b.recvEvenBval, "aux", b.recvAux, "binValues", b.binValues)

	// Ignore messages from older rounds.
	if msg.Round < b.round {
		b.aLogger.Debug("receive an aux msg from an older round", "replica", b.node.Id, "cur_round", b.round,
			"msg.Round", msg.Round)
		return nil
	}
	// Messages from later rounds will be qued and processed later.
	if msg.Round > b.round {
		b.aLogger.Debug("receive an aux msg from a future round", "replica", b.node.Id, "cur_round", b.round,
			"msg.Round", msg.Round)
		b.cachedAuxMsgs[b.round] = append(b.cachedAuxMsgs[b.round], msg)
		return nil
	}

	b.recvAux[msg.Sender] = msg.Value
	b.recvParSig = append(b.recvParSig, msg.TSPar)
	b.tryOutputAgreement(msg.SN)
	return nil
}

// tryOutputAgreement waits until at least (N - f) output messages received,
// once the (N - f) messages are received, make a common coin and uses it to
// compute the next decision estimate and output the optional decision value.
func (b *ABA) tryOutputAgreement(sn int) {
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
		[]byte(fmt.Sprint(b.round)), b.node.N-b.node.F, b.node.N)
	data := binary.BigEndian.Uint64(intactSig)
	coin := int(data % 2)
	b.aLogger.Debug("assemble the data and reveal the coin", "replica", b.node.Id, "round", b.round,
		"data", data, "coin", coin)

	if len(values) != 1 {
		if coin == values[0]%2 {
			b.estimated = values[0]
		} else {
			b.estimated = values[1]
		}
	} else {
		b.estimated = values[0]
		// Output can be set only once.
		if b.output == nil && values[0]%2 == coin {
			b.output = values[0]
			b.aLogger.Info("output the agreed value", "replica", b.node.Id, "value", values[0],
				"round", b.round)
			b.hasSentExitMsg = true
			msg := ABAExitMsg{
				SN:     sn,
				Sender: b.node.Id,
				Value:  values[0],
			}
			if err := b.node.PlainBroadcast(ABAExitMsgTag, msg, nil); err != nil {
				b.aLogger.Error(err.Error())
			}
		}
	}

	// Start the next round.
	b.aLogger.Debug("advancing to the next round after receiving aux messages", "replica", b.node.Id,
		"next_round", b.round+1, "aux_msg_count", lenOutputs)
	b.advanceRound()

	estimated := b.estimated.(int)
	if estimated%2 == 0 {
		b.sentBvals[b.round] = [2]bool{true, false}
	} else {
		b.sentBvals[b.round] = [2]bool{false, true}
	}

	msg := ABABvalRequestMsg{
		SN:     sn,
		Sender: b.node.Id,
		Round:  b.round,
		Value:  estimated,
	}
	if err := b.node.PlainBroadcast(ABABvalRequestMsgTag, msg, nil); err != nil {
		b.aLogger.Error(err.Error(), "replica", b.node.Id)
	}

	// process the cached messages for the next round.
	if cachedBvalMsgs, ok := b.cachedBvalMsgs[b.round]; ok {
		for _, cm := range cachedBvalMsgs {
			go func(m *ABABvalRequestMsg) {
				b.handleBvalRequest(m)
			}(cm)
		}
	}
	delete(b.cachedBvalMsgs, b.round)

	if cachedAuxMsgs, ok := b.cachedAuxMsgs[b.round]; ok {
		for _, cm := range cachedAuxMsgs {
			go func(m *ABAAuxRequestMsg) {
				b.handleAuxRequest(m)
			}(cm)
		}
	}
	delete(b.cachedAuxMsgs, b.round)
}

// countBvals counts all the received Bval inputs matching b.
// this function must be called in a mutex-protected env
func (b *ABA) countBvals(e int, round uint32) int {
	var toCheckBval map[int]bool
	if e%2 == 1 {
		toCheckBval = b.recvOddBval[round]
	} else {
		toCheckBval = b.recvEvenBval[round]
	}

	return len(toCheckBval)
}

// hasSentBval return true if we already sent out the given value.
func (b *ABA) hasSentBval(e int, round uint32) bool {
	sb := b.sentBvals[round]
	if e%2 == 0 {
		return sb[0]
	} else {
		return sb[1]
	}
}

// advanceRound will reset all the values that are bound to a round and increments
// the round value by 1.
func (b *ABA) advanceRound() {
	b.binValues = make(map[int]struct{})
	b.recvAux = make(map[int]int)
	b.recvParSig = [][]byte{}
	b.round++
	b.recvOddBval[b.round] = make(map[int]bool)
	b.recvEvenBval[b.round] = make(map[int]bool)
}

func (b *ABA) handleExitMessage(msg *ABAExitMsg) error {
	b.lock.Lock()
	defer b.lock.Unlock()

	b.exitMsgs[msg.Sender] = msg.Value
	lenEM := b.countExitMessages(msg.Value)
	if lenEM == b.node.F+1 && !b.hasSentExitMsg {
		b.hasSentExitMsg = true
		m := ABAExitMsg{
			SN:     msg.SN,
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
				"value", msg.Value, "round", b.round)
			b.output = msg.Value
		}
		b.done = true
		b.aLogger.Info("Return from ABA", "replica", b.node.Name, "output", b.output)
		go func() {
			b.node.statusChangeSignal <- StatusChangeSignal{
				SN:     msg.SN,
				Status: (b.node.status + 1) % STATUSCOUNT,
			}
		}()
	}
	return nil
}

// countExitMessages counts all the exitMessages matching v.
// this function must be called in a mutex-protected env
func (b *ABA) countExitMessages(e int) int {
	n := 0
	for _, val := range b.exitMsgs {
		if val == e {
			n++
		}
	}
	return n
}

// countAuxs returns the number of received (aux) messages, the corresponding
// values that where also in our inputs.
func (b *ABA) countAuxs() (int, []int) {
	valsMap := make(map[int]struct{})
	numQualifiedAux := 0
	for _, val := range b.recvAux {
		if _, ok := b.binValues[val]; ok {
			valsMap[val] = struct{}{}
			numQualifiedAux++
		}
	}

	var valsSet []int
	for val, _ := range valsMap {
		valsSet = append(valsSet, val)
	}

	return numQualifiedAux, valsSet
}
