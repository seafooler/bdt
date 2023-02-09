/**
Note that PB-related messages will not be cached
If a PB-related message is obsolete, either from a previous sn or a previous view, it will be omitted in the call
of processPBVALMsg or processPBVOTMsg;
If a PB-related message comes from the future, either from a future sn or a future view, it will be processed as well.
*/

package core

import (
	"github.com/seafooler/sign_tools"
	"sync"
)

type PB struct {
	spb *SPB

	id uint8

	pbOutputCh chan SMVBAQCedData

	dataToPB    []byte
	partialSigs [][]byte

	//delayedVALMsgs []*PBVALMessage
	//delayedTOVMsgs []*PBVOTMessage

	mux sync.RWMutex
}

func NewPB(s *SPB, id uint8) *PB {
	return &PB{
		spb: s,
		id:  id,
	}
}

func (pb *PB) PBBroadcastData(data, proof []byte, sn, view int, phase uint8) error {
	qcdChan := make(chan SMVBAQCedData)

	pb.mux.Lock()
	defer pb.mux.Unlock()

	// Although the node can process messages from different views at the same time,
	// each node will only start the SPB instance one by one.
	// Therefore, each time the function of PBBroadcastData is called, the PB variable should be reinitialized
	pb.pbOutputCh = qcdChan     // Each time a data is broadcast via pb, update the channel
	pb.partialSigs = [][]byte{} // Each time a data is broadcast via pb, clean partial signature slices
	pb.dataToPB = data          // Each time a data is broadcast via pb, update the dataToPB

	valMsg := SMVBAPBVALMessage{
		Data:   data,
		Proof:  proof,
		Dealer: pb.spb.s.node.Name,
		SMVBASNViewPhase: SMVBASNViewPhase{
			SMVBASNView: SMVBASNView{
				SN:   sn,
				View: view,
			},
			Phase: phase,
		},
	}

	if err := pb.spb.s.node.PlainBroadcast(SMVBAPBValTag, valMsg, nil); err != nil {
		return err
	}

	return nil
}

func (pb *PB) handlePBVALMsg(valMsg *SMVBAPBVALMessage) error {
	pb.spb.s.logger.Debug("handlePBVALMsg is called", "replica", pb.spb.s.node.Name,
		"snv", valMsg.SMVBASNView, "Dealer", valMsg.Dealer)

	dealerID := pb.spb.s.node.Name2IdMap[valMsg.Dealer]
	addrPort := pb.spb.s.node.Id2AddrMap[dealerID] + ":" + pb.spb.s.node.Id2PortMap[dealerID]

	// TODO: should sign over the data plus SNView rather than the only data
	partialSig := sign_tools.SignTSPartial(pb.spb.s.node.PriKeyTS, valMsg.Data)
	votMsg := SMVBAPBVOTMessage{
		PartialSig:       partialSig,
		Dealer:           valMsg.Dealer,
		Sender:           pb.spb.s.node.Name,
		SMVBASNViewPhase: valMsg.SMVBASNViewPhase,
		Data:             valMsg.Data,
	}

	go pb.spb.s.node.SendMsg(SMVBAPBVoteTag, votMsg, nil, addrPort)
	return nil
}

func (pb *PB) handlePBVOTMsg(votMsg *SMVBAPBVOTMessage) error {
	pb.spb.s.node.logger.Debug("HandlePBVOTMsg is called", "replica", pb.spb.s.node.Name,
		"pb.spb.node.snv", pb.spb.s.snv, "snv", votMsg.SMVBASNView, "id", pb.id, "sender",
		votMsg.Sender, "data", string(votMsg.Data))
	pb.mux.Lock()
	defer pb.mux.Unlock()

	// check again if sn and view is right
	// if receiving a vote message from a previous sn
	// a message with same sn but with a previous view will be considered in the next check,
	// since the previous view must be abandoned
	if votMsg.SN < pb.spb.s.snv.SN {
		return nil
	}

	// Check if the SPBs in this view are abandoned
	if pb.spb.s.snv.SN == votMsg.SN && pb.spb.s.abandon[votMsg.View] {
		return nil
	}

	pb.partialSigs = append(pb.partialSigs, votMsg.PartialSig)

	if len(pb.partialSigs) == pb.spb.s.node.N-pb.spb.s.node.F {
		intactSig := sign_tools.AssembleIntactTSPartial(pb.partialSigs, pb.spb.s.node.PubKeyTS,
			votMsg.Data, pb.spb.s.node.N-pb.spb.s.node.F, pb.spb.s.node.N)

		qcedData := SMVBAQCedData{
			Data:             pb.dataToPB,
			QC:               intactSig,
			SMVBASNViewPhase: votMsg.SMVBASNViewPhase,
		}

		pb.pbOutputCh <- qcedData
	}

	return nil
}
