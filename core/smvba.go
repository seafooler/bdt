package core

import (
	"encoding/binary"
	"fmt"
	"github.com/hashicorp/go-hclog"
	"github.com/seafooler/sign_tools"
	"sync"
)

type SMVBA struct {
	node *Node

	sync.Mutex

	snv SMVBASNView

	spb *SPB

	logger hclog.Logger

	inProcessData, inProcessProof []byte

	finishMessagesMap map[int]map[string]*SMVBAFinishMessage
	lockMessageMap    map[int]map[string]*SMVBAPBVALMessage

	// indicate if the done/elect message has been sent
	doneMessageMap  map[int]map[string]*SMVBADoneShareMessage
	doneMessageSent map[int]bool

	output   []byte
	outputCh chan SMVBASNView

	stopCh chan int

	abandon map[int]bool
	coin    map[int]int

	preVoteSent map[int]bool
	voteSent    map[int]bool

	preVoteMessageMap map[int]map[string]*SMVBAPreVoteMessage
	voteMessageMap    map[int]map[string]*SMVBAVoteMessage

	readyViewDataMap map[int]*SMVBAReadyViewData

	//cached messages from future MVBA rounds, map from sn to messages
	cachedFutureRoundMessagesMap map[int][]interface{}
}

func NewSMVBA(node *Node) *SMVBA {
	sMVBA := &SMVBA{
		node: node,

		snv: SMVBASNView{
			SN:   0,
			View: -1,
		},

		finishMessagesMap: make(map[int]map[string]*SMVBAFinishMessage),
		doneMessageMap:    make(map[int]map[string]*SMVBADoneShareMessage),
		doneMessageSent:   make(map[int]bool),
		lockMessageMap:    make(map[int]map[string]*SMVBAPBVALMessage),
		preVoteMessageMap: make(map[int]map[string]*SMVBAPreVoteMessage),
		voteMessageMap:    make(map[int]map[string]*SMVBAVoteMessage),
		preVoteSent:       make(map[int]bool),
		voteSent:          make(map[int]bool),
		abandon:           make(map[int]bool),
		readyViewDataMap:  make(map[int]*SMVBAReadyViewData),
		outputCh:          make(chan SMVBASNView),
		stopCh:            make(chan int),

		coin: make(map[int]int),

		cachedFutureRoundMessagesMap: make(map[int][]interface{}),
	}

	sMVBA.logger = hclog.New(&hclog.LoggerOptions{
		Name:   "bdt-smvba",
		Output: hclog.DefaultOutput,
		Level:  hclog.Level(node.LogLevel),
	})

	sMVBA.spb = NewSPB(sMVBA)

	return sMVBA
}

// OutputCh returns the outputCh variable
func (s *SMVBA) OutputCh() chan SMVBASNView {
	return s.outputCh
}

// Output returns the output variable
func (s *SMVBA) Output() []byte {
	return s.output
}

// StopCh returns the stopCh variable
func (s *SMVBA) StopCh() chan int {
	return s.stopCh
}

// InitializeInnerStatus cleans the inner status of a node and set SNView before starting the next round of MVBA
// This must be called with a wrap of lock
func (s *SMVBA) InitializeInnerStatus(sn int) {
	// No need to clean the status of SPB, since there is no inner status of SPB
	// and the inner status of PB is cleaned each time PBBroadcastData is called
	// s.spb = NewSPB(n)

	// cachedFutureRoundMessagesMap should not be cleaned

	s.snv = SMVBASNView{
		SN:   sn,
		View: -1,
	}

	s.finishMessagesMap = make(map[int]map[string]*SMVBAFinishMessage)
	s.lockMessageMap = make(map[int]map[string]*SMVBAPBVALMessage)

	s.doneMessageMap = make(map[int]map[string]*SMVBADoneShareMessage)
	s.doneMessageSent = make(map[int]bool)

	s.output = nil
	s.outputCh = make(chan SMVBASNView)
	s.stopCh = make(chan int)

	s.abandon = make(map[int]bool)
	s.coin = make(map[int]int)

	s.preVoteSent = make(map[int]bool)
	s.voteSent = make(map[int]bool)

	s.preVoteMessageMap = make(map[int]map[string]*SMVBAPreVoteMessage)
	s.voteMessageMap = make(map[int]map[string]*SMVBAVoteMessage)

	s.readyViewDataMap = make(map[int]*SMVBAReadyViewData)
}

// RetrieveCachedMessages retrieve the messages with sn from cachedFutureRoundMessagesMap
// This must be called with a wrap of lock
func (s *SMVBA) RetrieveCachedMessages(sn int) *SMVBAHaltMessage {
	msgs, ok := s.cachedFutureRoundMessagesMap[sn]
	if !ok {
		s.logger.Debug("No cached messages", "current-sn", s.snv.SN, "retrieve-sn", sn)
	} else {
		s.logger.Debug("Cached messages", "current-sn", s.snv.SN, "retrieve-sn", sn, "count", len(msgs))
	}

	// No PB-related messages are cached, whose reason can be found in the comments in pb.go
	var haltMsg *SMVBAHaltMessage
loop:
	for _, msg := range msgs {
		switch msgAsserted := msg.(type) {
		case *SMVBAFinishMessage:
			s.finishMessagesMap[msgAsserted.View] = make(map[string]*SMVBAFinishMessage)
			s.finishMessagesMap[msgAsserted.View][msgAsserted.Dealer] = msgAsserted
		case *SMVBADoneShareMessage:
			s.doneMessageMap[msgAsserted.View] = make(map[string]*SMVBADoneShareMessage)
			s.doneMessageMap[msgAsserted.View][msgAsserted.Sender] = msgAsserted
		case *SMVBAPreVoteMessage:
			s.preVoteMessageMap[msgAsserted.View] = make(map[string]*SMVBAPreVoteMessage)
			s.preVoteMessageMap[msgAsserted.View][msgAsserted.Sender] = msgAsserted
		case *SMVBAVoteMessage:
			s.voteMessageMap[msgAsserted.View] = make(map[string]*SMVBAVoteMessage)
			s.voteMessageMap[msgAsserted.View][msgAsserted.Sender] = msgAsserted
		case *SMVBAHaltMessage:
			// a halt message has received
			haltMsg = msgAsserted
			s.output = haltMsg.Value
			break loop
		}
	}

	// erase messages with sn from cachedFutureRoundMessagesMap
	delete(s.cachedFutureRoundMessagesMap, sn)
	return haltMsg
}

// BroadcastViaSPB encapsulates the inner functions of calling SPB
func (s *SMVBA) BroadcastViaSPB(data, proof []byte, sn, view int) (chan SMVBAQCedData, error) {
	return s.spb.SPBBroadcastData(data, proof, sn, view)
}

// VerifyTS verifies the data's threshold signature
func (s *SMVBA) VerifyTS(data, tsSig []byte) (bool, error) {
	return sign_tools.VerifyTS(s.node.PubKeyTS, data, tsSig)
}

// CompareSNView compares two SNViews
// if snv1 < snv2: return -1
// if snv1 = snv2: return 0
// if snv1 > snv2: return 1
func CompareSNView(snv1, snv2 SMVBASNView) int {
	if snv1.SN < snv2.SN {
		return -1
	}

	if snv1.SN == snv2.SN {
		if snv1.View < snv2.View {
			return -1
		} else if snv1.View == snv2.View {
			return 0
		} else {
			return 1
		}
	}

	return 1
}

// HandleFinishMsg processes the finish messages
func (s *SMVBA) HandleFinishMsg(fMsg *SMVBAFinishMessage) {
	s.logger.Debug("HandleFinishMsg is called", "replica", s.node.Name,
		"snv", fMsg.SMVBASNView, "dealer", fMsg.Dealer)

	// Check if output has been decided
	// This check need not wrapping with a lock, since it is used to accelerate stopping without sacrificing safety
	if fMsg.SN == s.snv.SN && s.output != nil {
		s.logger.Debug("HandleFinishMsg is called, but output has been decided", "replica", s.node.Name,
			"output", string(s.output))
		return
	}

	// Check if the Finish message is obsolete, the first check is used to accelerate the return from obsolete/future messages
	compResult := CompareSNView(fMsg.SMVBASNView, s.snv)
	if compResult == -1 {
		s.logger.Debug("Receive an obsolete Finish message", "replica", s.node.Name, "node.snv", s.snv,
			"msg.snv", fMsg.SMVBASNView)
		// No need to deal with an obsolete Finish message, just return
		// The reason is that if it is obsolete, the replica must have entered the next view successfully
		return
	}

	// doneShareMessage's SNView is assigned based on fMsg's SNView, so no mutex is needed here
	coinShare := sign_tools.SignTSPartial(s.node.PriKeyTS, []byte(fmt.Sprintf("%s%v",
		msgTagNameMap[SMVBADoneShareTag], fMsg.SMVBASNView)))
	doneShareMsg := SMVBADoneShareMessage{
		TSShare:     coinShare,
		Sender:      s.node.Name,
		SMVBASNView: fMsg.SMVBASNView,
	}

	s.Lock()
	defer s.Unlock()

	// check SN
	if fMsg.SN < s.snv.SN {
		return
	} else if fMsg.SN > s.snv.SN {
		if _, ok := s.cachedFutureRoundMessagesMap[fMsg.SN]; !ok {
			s.cachedFutureRoundMessagesMap[fMsg.SN] = []interface{}{fMsg}
		} else {
			s.cachedFutureRoundMessagesMap[fMsg.SN] = append(s.cachedFutureRoundMessagesMap[fMsg.SN], fMsg)
		}
		return
	}

	if _, ok := s.finishMessagesMap[fMsg.View]; !ok {
		s.finishMessagesMap[fMsg.View] = make(map[string]*SMVBAFinishMessage)
	}
	s.finishMessagesMap[fMsg.View][fMsg.Dealer] = fMsg
	s.logger.Debug("FinishMessageMap inserted", "replica", s.node.Name, "dealer", fMsg.Dealer)
	if len(s.finishMessagesMap[fMsg.View]) == 2*s.node.F+1 {
		if !s.doneMessageSent[fMsg.View] {
			s.doneMessageSent[fMsg.View] = true
			go func() {
				if err := s.node.PlainBroadcast(SMVBADoneShareTag, doneShareMsg, nil); err != nil {
					s.logger.Error(err.Error())
				}
			}()
		}
		// If coin is already revealed, it indicates that it is waiting for 2f+1 Finish messages
		if coin, ok := s.coin[fMsg.View]; ok {
			// Both 2f+1 Finish messages and 2f+1 Done messages are collected, abandon all the SPBs corresponding to msg.View
			s.abandon[fMsg.View] = true
			s.logger.Debug("Abandon all the SPBs", "replica", s.node.Name, "s.SNV", s.snv, "fMsg.View", fMsg.View)

			coin2NodeName := s.node.Id2NameMap[coin]
			s.HaltOrPreVote(fMsg.SMVBASNView, coin2NodeName)
		}
	}
}

func (s *SMVBA) RunOneMVBAView(usePrevData bool, data, proof []byte, snv SMVBASNView) error {
	s.logger.Debug("RunOneMVBAView is called", "replica", s.node.Name, "snv", snv)
	s.Lock()
	// check if output has been decided
	if snv.SN == s.snv.SN && s.output != nil {
		s.logger.Debug("RunOneMVBAView is called, but output has been decided", "replica", s.node.Name,
			"output", string(s.output))
		s.Unlock()
		return nil
	}
	// check if the previous view is currently processed
	compResult := CompareSNView(snv, s.snv)
	if compResult == -1 {
		s.logger.Debug("RunOneMVBAView with an obsolete call", "replica", s.node.Name, "node.snv", s.snv, "call.snv", snv)
		s.Unlock()
		return nil
	} else if compResult == 1 {
		s.logger.Debug("RunOneMVBAView receives a future call", "replica", s.node.Name, "node.snv", s.snv, "call.snv", snv)
		// cache the call
		s.readyViewDataMap[snv.View] = &SMVBAReadyViewData{
			usePrevData: usePrevData,
			data:        data,
			proof:       proof,
		}
		s.Unlock()
		return nil
	}

	s.logger.Debug("RunOneMVBAView with a correct snv", "replica", s.node.Name, "snv", snv)

	s.AdvanceView()
	if !usePrevData {
		s.inProcessData = data
	}
	s.inProcessProof = proof
	s.Unlock()

	//s.logger.Debug("s.readyViewDataMap", "replica", s.node.Name, "s.readyViewDataMap", s.readyViewDataMap,
	//"s.snv.View", s.snv.View)

	// Check if there is a cached readyViewData
	if rvd, ok := s.readyViewDataMap[s.snv.View]; ok {
		s.logger.Debug("Retrieve a cached readyViewData and run the next view directly", "replica", s.node.Name,
			"s.SNView", s.snv)
		go s.RunOneMVBAView(rvd.usePrevData, rvd.data, rvd.proof, s.snv)
		return nil
	}

	// Phase 1: SPB phase
	s.logger.Debug("Run a new view by calling the SPB", "replica", s.node.Name, "s.SNView", s.snv,
		"data", string(s.inProcessData), "usePrevData", usePrevData)
	qcedDataCh, err := s.BroadcastViaSPB(s.inProcessData, s.inProcessProof, s.snv.SN, s.snv.View)
	if err != nil {
		return err
	}

	// Phase 2: Broadcast finish messages

	// TODO: there may be a bug here, what if qcedDataCh never outputs
	// Fix: if it receives a signal from the stopCh, it will return
	var qcedData SMVBAQCedData
	select {
	case qcedData = <-qcedDataCh:
	case <-s.stopCh:
		s.logger.Debug("This round of MVBA is stopped", "replica", s.node.Name, "sn", snv.SN)
		return nil
	}

	finishMsg := SMVBAFinishMessage{
		Data:        qcedData.Data,
		QC:          qcedData.QC,
		Dealer:      s.node.Name,
		SMVBASNView: s.snv,
	}

	s.logger.Debug("finishMsg's Data", "replica", s.node.Name,
		"finishMsg.Data", string(finishMsg.Data))

	if err := s.node.PlainBroadcast(SMVBAFinishTag, finishMsg, nil); err != nil {
		return err
	}

	return nil
}

func (s *SMVBA) AdvanceView() {
	s.logger.Info("+++++++++++++++ advance to the next view", "SN", s.snv.SN, "replica", s.node.Name,
		"next-view", s.snv.View+1)
	s.snv.View = s.snv.View + 1

}

func (s *SMVBA) HandleDoneShareMsg(msg *SMVBADoneShareMessage) {
	s.logger.Debug("HandleDoneShareMsg is called", "replica", s.node.Name,
		"snv", msg.SMVBASNView, "sender", msg.Sender)
	// Check if output has been decided
	// This check need not wrapping with a lock, since it is used to accelerate stopping without sacrificing safety
	if msg.SN == s.snv.SN && s.output != nil {
		s.logger.Debug("HandleDoneShareMsg is called, but output has been decided", "replica", s.node.Name,
			"output", string(s.output))
		return
	}

	coinShare := sign_tools.SignTSPartial(s.node.PriKeyTS, []byte(fmt.Sprintf("%s%v",
		msgTagNameMap[SMVBADoneShareTag], msg.SMVBASNView)))
	doneShareMsg := SMVBADoneShareMessage{
		TSShare:     coinShare,
		Sender:      s.node.Name,
		SMVBASNView: msg.SMVBASNView,
	}

	// store the Done/Share messages
	s.Lock()
	defer s.Unlock()

	// check SN
	if msg.SN < s.snv.SN {
		return
	} else if msg.SN > s.snv.SN {
		if _, ok := s.cachedFutureRoundMessagesMap[msg.SN]; !ok {
			s.cachedFutureRoundMessagesMap[msg.SN] = []interface{}{msg}
		} else {
			s.cachedFutureRoundMessagesMap[msg.SN] = append(s.cachedFutureRoundMessagesMap[msg.SN], msg)
		}
		return
	}

	if _, ok := s.doneMessageMap[msg.View]; !ok {
		s.doneMessageMap[msg.View] = make(map[string]*SMVBADoneShareMessage)
	}
	s.doneMessageMap[msg.View][msg.Sender] = msg
	// Amplify broadcasting done/share messages
	if len(s.doneMessageMap[msg.View]) == s.node.F+1 && !s.doneMessageSent[msg.View] {
		s.doneMessageSent[msg.View] = true
		go func() {
			if err := s.node.PlainBroadcast(SMVBADoneShareTag, doneShareMsg, nil); err != nil {
				s.logger.Error(err.Error())
			}
		}()
	}

	// if more than 2f+1 Done/Share messages are collected, reveal the coin
	if len(s.doneMessageMap[msg.View]) != 2*s.node.F+1 {
		return
	}
	partialSigs := make([][]byte, 2*s.node.F+1)
	count := 0
	for _, dsMsg := range s.doneMessageMap[msg.View] {
		partialSigs[count] = dsMsg.TSShare
		count++
		if count == 2*s.node.F+1 {
			break
		}
	}
	data := []byte(fmt.Sprintf("%s%v", msgTagNameMap[SMVBADoneShareTag], msg.SMVBASNView))
	intactTS := sign_tools.AssembleIntactTSPartial(partialSigs, s.node.PubKeyTS, data, 2*s.node.F+1, s.node.N)
	coin := binary.BigEndian.Uint64(intactTS) % uint64(s.node.N)

	s.coin[msg.View] = int(coin)
	s.logger.Debug("Coin is revealed", "SN", msg.SN, "View", msg.View, "replica", s.node.Name,
		"coin", coin)

	s.logger.Debug("The current finishMessageMap status", "replica", s.node.Name, "finishMessageMap", s.finishMessagesMap)

	// Check if the Finish message corresponding to the coin is received.
	coin2NodeName := s.node.Id2NameMap[int(coin)]

	// Check if more than 2f+1 Finish messages are received
	// TODO: Here, we do not need its own SPB is finished, which may need checking
	if _, ok := s.finishMessagesMap[msg.View]; !ok {
		return
	}
	if len(s.finishMessagesMap[msg.View]) < 2*s.node.F+1 {
		return
	}

	// Both 2f+1 Finish messages and 2f+1 Done messages are collected, abandon all the SPBs corresponding to msg.View
	s.abandon[msg.View] = true
	s.logger.Debug("Abandon all the SPBs", "replica", s.node.Name, "s.SNV", s.snv, "msg.View", msg.View)

	s.HaltOrPreVote(msg.SMVBASNView, coin2NodeName)
}

// HaltOrPreVote must be called by a wrapped lock
func (s *SMVBA) HaltOrPreVote(snv SMVBASNView, coinNode string) {
	s.logger.Debug("HaltOrPreVote is called", "replica", s.node.Name,
		"snv", snv)
	finishMsgByCoin := s.finishMessagesMap[snv.View][coinNode]
	if finishMsgByCoin != nil && s.output == nil {
		// If true, output it
		s.output = finishMsgByCoin.Data
		s.logger.Debug("Data is output after consensus", "replica", s.node.Name, "SNView", snv,
			"dealer", finishMsgByCoin.Dealer, "data", string(s.output))
		// broadcast halt messages
		hm := SMVBAHaltMessage{
			Value:       finishMsgByCoin.Data,
			Proof:       finishMsgByCoin.QC,
			SMVBASNView: snv,
			Dealer:      finishMsgByCoin.Dealer,
		}

		go s.node.PlainBroadcast(SMVBAHaltTag, hm, nil)

		// send a signal to suspend this round of MVBA or receiving a signal than MVBA has been suspended
		s.logger.Debug("Before sending a signal to outputCh or receiving a signal from stopCh in HaltOrPreVote",
			"replica", s.node.Name, "snv", snv)
		select {
		case s.outputCh <- snv:
			s.logger.Debug("Successfully send a signal to outputCh in HaltOrPreVote",
				"replica", s.node.Name, "snv", snv)
		case sn := <-s.stopCh:
			s.logger.Debug("To send a signal to outputCh in HaltOrPreVote, while finding MVBA is stopped",
				"replica", s.node.Name, "sn", sn)
		}

	} else if !s.preVoteSent[snv.View] {
		s.preVoteSent[snv.View] = true
		s.logger.Debug("Broadcast PreVote message", "replica", s.node.Name, "SNV", snv)
		// No Finish message corresponding to the coin is received, start the view change
		go s.BroadcastPreVote(snv)
	}
}

func (s *SMVBA) BroadcastPreVote(snv SMVBASNView) error {
	s.logger.Debug("BroadcastPreVote is called", "replica", s.node.Name,
		"snv", snv)
	// Attempt to fetch the lock data corresponding to coin
	coin2NodeName := s.node.Id2NameMap[s.coin[snv.View]] // s.coin[snv.View] will never be nil
	// TODO: bug?
	lockData, ok := s.lockMessageMap[snv.View][coin2NodeName]
	pvm := SMVBAPreVoteMessage{
		Dealer:      coin2NodeName,
		Sender:      s.node.Name,
		SMVBASNView: snv,
	}
	if ok {
		// lock data exists
		pvm.Flag = true
		pvm.Value = lockData.Data
		pvm.ProofOrPartialSig = lockData.Proof
	} else {
		pvm.Flag = false
		pvm.Value = nil
		pvm.ProofOrPartialSig = sign_tools.SignTSPartial(s.node.PriKeyTS, []byte(fmt.Sprintf("%s%v",
			msgTagNameMap[SMVBAPreVoteTag], snv)))
	}

	go s.node.PlainBroadcast(SMVBAPreVoteTag, pvm, nil)
	return nil
}

func (s *SMVBA) HandlePreVoteMsg(pvm *SMVBAPreVoteMessage) {
	s.logger.Debug("HandlePreVoteMsg is called", "replica", s.node.Name,
		"snv", pvm.SMVBASNView, "sender", pvm.Sender)

	// Check if output has been decided
	// This check need not wrapping with a lock, since it is used to accelerate stopping without sacrificing safety
	if pvm.SN == s.snv.SN && s.output != nil {
		s.logger.Debug("HandlePreVoteMsg is called, but output has been decided", "replica", s.node.Name,
			"output", string(s.output))
		return
	}

	coin2NodeName := s.node.Id2NameMap[s.coin[pvm.View]]
	vm := SMVBAVoteMessage{
		Dealer:      coin2NodeName,
		Sender:      s.node.Name,
		SMVBASNView: pvm.SMVBASNView,
	}

	// Check if pvm has a flag of true
	if pvm.Flag {
		// TODO: check the proof of PVM
		vm.Flag = true
		vm.Value = pvm.Value
		vm.Proof = pvm.ProofOrPartialSig
		vm.Pho = sign_tools.SignTSPartial(s.node.PriKeyTS, []byte(fmt.Sprintf("%s%v%T",
			msgTagNameMap[SMVBAVoteTag], pvm.SMVBASNView, true)))

		s.Lock()
		// check SN
		if pvm.SN < s.snv.SN {
			s.Unlock()
			return
		} else if pvm.SN > s.snv.SN {
			if _, ok := s.cachedFutureRoundMessagesMap[pvm.SN]; !ok {
				s.cachedFutureRoundMessagesMap[pvm.SN] = []interface{}{pvm}
			} else {
				s.cachedFutureRoundMessagesMap[pvm.SN] = append(s.cachedFutureRoundMessagesMap[pvm.SN], pvm)
			}
			s.Unlock()
			return
		}

		if !s.voteSent[pvm.View] {
			s.voteSent[pvm.View] = true
			go s.node.PlainBroadcast(SMVBAVoteTag, vm, nil)
			s.Unlock()
			return
		}
		s.Unlock()
	}

	s.Lock()
	defer s.Unlock()
	// check SN
	if pvm.SN < s.snv.SN {
		return
	} else if pvm.SN > s.snv.SN {
		if _, ok := s.cachedFutureRoundMessagesMap[pvm.SN]; !ok {
			s.cachedFutureRoundMessagesMap[pvm.SN] = []interface{}{pvm}
		} else {
			s.cachedFutureRoundMessagesMap[pvm.SN] = append(s.cachedFutureRoundMessagesMap[pvm.SN], pvm)
		}
		return
	}

	if _, ok := s.preVoteMessageMap[pvm.View]; !ok {
		s.preVoteMessageMap[pvm.View] = make(map[string]*SMVBAPreVoteMessage)
	}
	s.preVoteMessageMap[pvm.View][pvm.Sender] = pvm
	if len(s.preVoteMessageMap[pvm.View]) == 2*s.node.F+1 && !s.voteSent[pvm.View] {
		s.voteSent[pvm.View] = true
		vm.Flag = false
		vm.Value = nil
		partialSigs := make([][]byte, 2*s.node.F+1)
		count := 0
		for _, msg := range s.preVoteMessageMap[pvm.View] {
			partialSigs[count] = msg.ProofOrPartialSig
			count++
			if count == 2*s.node.F+1 {
				break
			}
		}
		data := []byte(fmt.Sprintf("%s%v", msgTagNameMap[SMVBAPreVoteTag], pvm.SMVBASNView))
		intactTS := sign_tools.AssembleIntactTSPartial(partialSigs, s.node.PubKeyTS,
			data, s.node.N-s.node.F, s.node.N)
		vm.Proof = intactTS
		vm.Pho = sign_tools.SignTSPartial(s.node.PriKeyTS, []byte(fmt.Sprintf("%s%v%T",
			msgTagNameMap[SMVBAVoteTag], pvm.SMVBASNView, false)))
		go s.node.PlainBroadcast(SMVBAVoteTag, vm, nil)
	}
}

func (s *SMVBA) HandleVoteMsg(vm *SMVBAVoteMessage) {
	s.logger.Debug("HandleVoteMsg is called", "replica", s.node.Name,
		"snv", vm.SMVBASNView, "sender", vm.Sender)
	// Check if output has been decided
	// This check need not wrapping with a lock, since it is used to accelerate stopping without sacrificing safety
	if vm.SN == s.snv.SN && s.output != nil {
		s.logger.Debug("HandleVoteMsg is called, but output has been decided", "replica", s.node.Name,
			"output", string(s.output))
		return
	}

	s.Lock()
	defer s.Unlock()

	// check SN
	if vm.SN < s.snv.SN {
		return
	} else if vm.SN > s.snv.SN {
		if _, ok := s.cachedFutureRoundMessagesMap[vm.SN]; !ok {
			s.cachedFutureRoundMessagesMap[vm.SN] = []interface{}{vm}
		} else {
			s.cachedFutureRoundMessagesMap[vm.SN] = append(s.cachedFutureRoundMessagesMap[vm.SN], vm)
		}
		return
	}

	// No need to deal with an obsolete Vote message, just return
	// The reason is that if it is obsolete, the replica must have entered the next view successfully
	if vm.View < s.snv.View {
		return
	}
	if _, ok := s.voteMessageMap[vm.View]; !ok {
		s.voteMessageMap[vm.View] = make(map[string]*SMVBAVoteMessage)
	}
	s.voteMessageMap[vm.View][vm.Sender] = vm
	usePrevData := false
	if len(s.voteMessageMap[vm.View]) == 2*s.node.F+1 {
		trueCount := 0
		falseCount := 0
		partialSigsTrue := make([][]byte, 2*s.node.F+1)
		partialSigsFalse := make([][]byte, 2*s.node.F+1)
		var someVoteMsgTrue *SMVBAVoteMessage
		for _, m := range s.voteMessageMap[vm.View] {
			if m.Flag {
				partialSigsTrue[trueCount] = m.Pho
				trueCount++
				someVoteMsgTrue = m
			} else {
				partialSigsFalse[falseCount] = m.Pho
				falseCount++
			}
		}

		// 2f+1 true votes
		if trueCount == 2*s.node.F+1 && s.output == nil {
			// decide and output it
			s.output = vm.Value
			s.logger.Debug("Data is output after consensus", "replica", s.node.Name, "sn", vm.SN, "view", vm.View,
				"dealer", vm.Dealer, "data", string(s.output))

			data := []byte(fmt.Sprintf("%s%v%T", msgTagNameMap[SMVBAVoteTag], vm.SMVBASNView, true))
			intactTS := sign_tools.AssembleIntactTSPartial(partialSigsTrue, s.node.PubKeyTS,
				data, s.node.N-s.node.F, s.node.N)

			// broadcast halt messages
			hm := SMVBAHaltMessage{
				Value:       vm.Value,
				Proof:       intactTS,
				SMVBASNView: vm.SMVBASNView,
				Dealer:      vm.Dealer,
			}

			go s.node.PlainBroadcast(SMVBAHaltTag, hm, nil)
			// send a signal to suspend this round of MVBA
			s.logger.Debug("Before sending a signal to outputCh or receiving a signal from stopCh in HandleVoteMsg",
				"replica", s.node.Name, "snv", vm.SMVBASNView)
			select {
			case s.outputCh <- vm.SMVBASNView:
				s.logger.Debug("Successfully send a signal to outputCh in HandleVoteMsg",
					"replica", s.node.Name, "snv", vm.SMVBASNView)
			case sn := <-s.stopCh:
				s.logger.Debug("To send signal to outputCh in HandleVoteMsg, while finding MVBA is stopped",
					"replica", s.node.Name, "sn", sn)
			}
			return
		}

		var dataForNewView, proofForNewView []byte
		if falseCount == 2*s.node.F+1 {
			// 2f+1 false votes
			data := []byte(fmt.Sprintf("%s%v%T", msgTagNameMap[SMVBAVoteTag], vm.SMVBASNView, false))
			intactTS := sign_tools.AssembleIntactTSPartial(partialSigsFalse, s.node.PubKeyTS,
				data, s.node.N-s.node.F, s.node.N)
			usePrevData = true
			// dataForNewView = s.inProcessData
			// TODO: proofForNewView is different from the paper description
			proofForNewView = intactTS
		} else {
			dataForNewView = someVoteMsgTrue.Value
			proofForNewView = someVoteMsgTrue.Proof
		}

		go s.RunOneMVBAView(usePrevData, dataForNewView, proofForNewView, vm.SMVBASNView)

	}

}

func (s *SMVBA) HandleHaltMsg(hm *SMVBAHaltMessage) {
	s.logger.Debug("HandleHaltMsg is called", "replica", s.node.Name,
		"snv", hm.SMVBASNView, "dealer", hm.Dealer)
	s.Lock()
	defer s.Unlock()
	//TODO: check the proof in HaltMessage

	//check SN
	if hm.SN < s.snv.SN {
		return
	} else if hm.SN > s.snv.SN {
		if _, ok := s.cachedFutureRoundMessagesMap[hm.SN]; !ok {
			s.cachedFutureRoundMessagesMap[hm.SN] = []interface{}{hm}
		} else {
			s.cachedFutureRoundMessagesMap[hm.SN] = append(s.cachedFutureRoundMessagesMap[hm.SN], hm)
		}
		return
	}

	if s.output == nil {
		s.output = hm.Value
		s.logger.Debug("Data is output after consensus by receiving a halt message ", "replica", s.node.Name,
			"sn", hm.SN, "node-view", s.snv.View, "dealer", hm.Dealer, "data", string(s.output))
		go s.node.PlainBroadcast(SMVBAHaltTag, *hm, nil)
		// send a signal to suspend this round of MVBA
		s.logger.Debug("Before sending a signal to outputCh or receiving a signal from stopCh in HandleHaltMsg",
			"replica", s.node.Name, "snv", hm.SMVBASNView)
		select {
		case s.outputCh <- s.snv:
			s.logger.Debug("Successfully send a signal to outputCh in HandleHaltMsg",
				"replica", s.node.Name, "snv", hm.SMVBASNView)
		case sn := <-s.stopCh:
			s.logger.Debug("To send signal to outputCh in HandleHaltMsg, while finding MVBA is stopped",
				"replica", s.node.Name, "sn", sn)
		}

	}
}
