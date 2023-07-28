package core

import (
	"github.com/hashicorp/go-hclog"
	"github.com/seafooler/sign_tools"
	"sync"
	"time"
)

type Bolt struct {
	node *Node

	bLogger  hclog.Logger
	leaderId int

	committedHeight      int
	maxProofedHeight     int
	proofedHeight        map[int][][HASHSIZE]byte
	cachedHeight         map[int][][HASHSIZE]byte
	cachedVoteMsgs       map[int]map[int][]byte
	cachedBlockProposals map[int]*BoltProposalMsg

	proofReady chan ProofData

	paceSyncMsgsReceived map[int]struct{}
	paceSyncMsgSent      bool
	sync.Mutex
}

func NewBolt(node *Node, leader int) *Bolt {
	return &Bolt{
		node: node,
		bLogger: hclog.New(&hclog.LoggerOptions{
			Name:       "bdt-bolt",
			Output:     hclog.DefaultOutput,
			TimeFormat: "2006-01-02 15:04:05.000000",
			Level:      hclog.Level(node.Config.LogLevel),
		}),
		leaderId:             leader,
		committedHeight:      0,
		proofedHeight:        make(map[int][][HASHSIZE]byte),
		cachedHeight:         make(map[int][][HASHSIZE]byte),
		cachedVoteMsgs:       make(map[int]map[int][]byte),
		cachedBlockProposals: make(map[int]*BoltProposalMsg),
		paceSyncMsgsReceived: make(map[int]struct{}),
		proofReady:           make(chan ProofData),
	}
}

func (b *Bolt) ProposalLoop(startHeight int) {
	//b.node.lastBlockCreatedTime = time.Now()
	if b.node.Id == b.leaderId {
		go func() {
			b.proofReady <- ProofData{
				Proof:  nil,
				Height: startHeight - 1,
			}
		}()
	}

	for {
		proofReady := <-b.proofReady
		//curTime := time.Now()
		//estimatdTxNum := int(curTime.Sub(b.node.lastBlockCreatedTime).Seconds() * float64(b.node.Config.Rate))
		//if estimatdTxNum > b.node.maxCachedTxs {
		//	estimatdTxNum = b.node.maxCachedTxs
		//}
		//
		//b.node.lastBlockCreatedTime = curTime

		// the next leader is height%n
		if b.node.Id != (proofReady.Height+1)%b.node.N {
			b.bLogger.Info("b.node.Id != (proofReady.Height+1)%b.node.N", "b.node.Id", b.node.Id,
				"(proofReady.Height+1)%b.node.N", (proofReady.Height+1)%b.node.N, "proofReady.Height", proofReady.Height,
				"b.node.N", b.node.N)
			continue
		}

		b.node.Lock()
		payLoadHashes, cnt := b.node.createBlock(true)
		//println("height:" + strconv.Itoa(proofReady.Height+1))
		//println("length" + strconv.Itoa(cnt))
		b.node.Unlock()

		newBlock := &Block{
			SN:            proofReady.SN,
			TxNum:         cnt * b.node.maxNumInPayLoad,
			PayLoadHashes: payLoadHashes,
			Height:        proofReady.Height + 1,
			Proposer:      b.node.Id,
		}

		//simulate the ddos attack to the leader
		// test the switch from opt to pes and back, so only the first sn is ddos
		if b.node.DDoS && b.node.sn == 0 {
			time.Sleep(time.Millisecond * time.Duration(b.node.Config.DDoSDelay))
		}

		if err := b.BroadcastProposalProof(newBlock, proofReady.Proof); err != nil {
			b.bLogger.Error("fail to broadcast proposal and proof", "height", newBlock.Height,
				"err", err.Error())
		} else {
			b.bLogger.Info("successfully broadcast a new proposal and proof", "sn", newBlock.SN,
				"height", newBlock.Height, "payloadCnt", len(payLoadHashes))
		}
	}
}

// BroadcastProposalProof broadcasts the new block and proof of previous block through the ProposalMsg
func (b *Bolt) BroadcastProposalProof(blk *Block, proof map[int][]byte) error {
	proposalMsg := BoltProposalMsg{
		Block: *blk,
		Proof: proof,
	}
	if err := b.node.PlainBroadcast(BoltProposalMsgTag, proposalMsg, nil); err != nil {
		return err
	}
	return nil
}

// ProcessBoltProposalMsg votes for the current proposal and commits the previous-previous block
func (b *Bolt) ProcessBoltProposalMsg(pm *BoltProposalMsg) error {
	b.bLogger.Debug("Process the Bolt Proposal Message", "sn", pm.SN, "block_index", pm.Height)
	b.Lock()
	defer b.Unlock()
	b.cachedHeight[pm.Height] = pm.PayLoadHashes
	b.cachedBlockProposals[pm.Height] = pm
	// do not retrieve the previous block nor verify the proof for the 0th block
	// try to cache a previous block
	b.bLogger.Debug("pm message", "height", pm.Height, "plhashes.len", len(pm.PayLoadHashes))

	b.tryCache(pm.Height, pm.Proof, pm.PayLoadHashes)

	// if there is already a subsequent block, deal with it
	if plHashes, ok := b.cachedHeight[pm.Height+1]; ok {
		b.tryCache(pm.Height+1, b.cachedBlockProposals[pm.Height+1].Proof, plHashes)
	}

	// try to commit a pre-previous block
	b.tryCommit(pm.SN, pm.Height)

	// if there is a subsequent-subsequent block, deal with it
	if _, ok := b.proofedHeight[pm.Height+2]; ok {
		b.tryCommit(pm.SN, pm.Height+2)
	}

	// create the ts share of new block
	blockBytes, err := encode(pm.Block)
	if err != nil {
		b.bLogger.Error("fail to encode the block", "block_index", pm.Height)
		return err
	}
	//share := sign_tools.SignTSPartial(b.node.PriKeyTS, blockBytes)

	sig := sign_tools.SignEd25519(b.node.Config.PriKeyED, blockBytes)

	// send the ts share to the leader
	boltVoteMsg := BoltVoteMsg{
		SN:     pm.SN,
		EDSig:  sig,
		Height: pm.Height,
		Voter:  b.node.Id,
	}
	// the next leader is (pm.Height+1)%b.node.N
	leaderAddrPort := b.node.Id2AddrMap[(pm.Height+1)%b.node.N] + ":" + b.node.Id2PortMap[(pm.Height+1)%b.node.N]
	err = b.node.SendMsg(BoltVoteMsgTag, boltVoteMsg, nil, leaderAddrPort)
	if err != nil {
		b.bLogger.Error("fail to vote for the block", "block_index", pm.Height)
		return err
	} else {
		b.bLogger.Debug("successfully vote for the block", "block_index", pm.Height)
	}
	if b.node.Id == pm.Height%b.node.N {
		return b.tryAssembleProof(pm.Height)
	} else {
		return nil
	}
}

// tryCache must be wrapped in a lock
func (b *Bolt) tryCache(height int, proof map[int][]byte, plHashes [][HASHSIZE]byte) error {
	// retrieve the previous block
	pBlk, ok := b.cachedBlockProposals[height-1]
	if !ok {
		// Fixed: Todo: deal with the situation where the previous block has not been cached
		b.bLogger.Info("did not cache the previous block", "prev_block_index", height-1)
		// This is not a bug, maybe a previous block has not been cached
		return nil
	}

	//verify the proof
	blockBytes, err := encode(pBlk.Block)
	if err != nil {
		b.bLogger.Error("fail to encode the block", "block_index", height)
		return err
	}

	if len(proof) < b.node.N-b.node.F {
		b.bLogger.Error("the number of signatures in the proof is not enough", "needed", b.node.N-b.node.F,
			"len(proof)", len(proof))
		return nil
	}

	for i, sig := range proof {
		if _, err := sign_tools.VerifySignEd25519(b.node.PubKeyED[i], blockBytes, sig); err != nil {
			b.bLogger.Error("fail to verify proof of a previous block", "prev_block_index", pBlk.Height)
			return err
		}
	}

	//if _, err := sign_tools.VerifyTS(b.node.PubKeyTS, blockBytes, proof); err != nil {
	//	b.bLogger.Error("fail to verify proof of a previous block", "prev_block_index", pBlk.Height)
	//	return err
	//}

	b.proofedHeight[pBlk.Height] = pBlk.PayLoadHashes
	b.maxProofedHeight = pBlk.Height
	delete(b.cachedHeight, pBlk.Height)

	b.node.Lock()
	for _, hx := range plHashes {
		b.node.proposedPayloads[hx] = true
	}
	b.node.Unlock()

	// if there is already a subsequent block, deal with it
	if hashes, ok := b.cachedHeight[height+1]; ok {
		b.tryCache(height+1, b.cachedBlockProposals[height+1].Proof, hashes)
	}

	return nil
}

// tryCommit must be wrapped in a lock
func (b *Bolt) tryCommit(sn, height int) error {
	if payLoadHashes, ok := b.proofedHeight[height-2]; ok {
		committedCount := 0
		b.node.Lock()
		for _, plHash := range payLoadHashes {
			if _, okk := b.node.PayLoadSavings[plHash]; okk {
				if _, ok := b.node.payloadMine[plHash]; ok {
					delete(b.node.payloadMine, plHash)
					delete(b.node.payLoads, plHash)
					delete(b.node.proposedPayloads, plHash)
					b.node.committedPayloads[plHash] = true
					committedCount++
					b.bLogger.Info("Payload commitInfo:", "height", height-2,
						"TXnums", b.node.PayLoadSavings[plHash].Info.Count,
						"timestamp", b.node.PayLoadSavings[plHash].Info.TimeStamp)
				} else if _, ok := b.node.payLoads[plHash]; ok {
					delete(b.node.payLoads, plHash)
					delete(b.node.proposedPayloads, plHash)
					b.node.committedPayloads[plHash] = true
					committedCount++
					b.bLogger.Info("Payload commitInfo:", "height", height-2,
						"TXnums", b.node.PayLoadSavings[plHash].Info.Count,
						"timestamp", b.node.PayLoadSavings[plHash].Info.TimeStamp)
				} else if _, existed := b.node.committedPayloads[plHash]; existed {
					b.bLogger.Info("PayLoad has been committed!")
					// b.node.committedPayloads[plHash] = true
					// committedCount++
				}
			}
		}
		b.node.Unlock()
		b.bLogger.Info("Commit a block in Bolt", "name", b.node.Name, "sn", sn, "block_index", height-2,
			"committed_payload_cnt", committedCount, "payload_after_commit", len(b.node.payLoads), "total_commit_payload", len(b.node.committedPayloads))
		// Todo: check the consecutive commitment
		b.committedHeight = height - 2
		delete(b.proofedHeight, height-2)
	}

	// if there is a subsequent-subsequent block, deal with it
	if _, ok := b.proofedHeight[height+2]; ok {
		b.tryCommit(sn, height+2)
	}

	return nil
}

// ProcessBoltVoteMsg stores the vote messages and attempts to create the ts proof
func (b *Bolt) ProcessBoltVoteMsg(vm *BoltVoteMsg) error {
	b.bLogger.Debug("Process the Bolt Vote Message", "block_index", vm.Height)
	b.Lock()
	defer b.Unlock()
	if _, ok := b.cachedVoteMsgs[vm.Height]; !ok {
		b.cachedVoteMsgs[vm.Height] = make(map[int][]byte)
	}
	b.cachedVoteMsgs[vm.Height][vm.Voter] = vm.EDSig
	return b.tryAssembleProof(vm.Height)
}

// tryAssembleProof must be wrapped in a lock
// tryAssembleProof may be called by ProcessBoltVoteMsg() or ProcessBoltProposalMsg()
func (b *Bolt) tryAssembleProof(height int) error {
	if len(b.cachedVoteMsgs[height]) == b.node.N-b.node.F {
		shares := make(map[int][]byte)
		cnt := 0
		for i, share := range b.cachedVoteMsgs[height] {
			shares[i] = share
			cnt++
			if cnt >= b.node.N-b.node.F {
				break
			}
		}

		//cBlk, ok := b.cachedBlockProposals[height]
		//if !ok {
		//	b.bLogger.Debug("cachedBlocks does not contain the block", "b.cachedBlocks", b.cachedBlockProposals,
		//		"vm.Height", height)
		//	// This is not an error, since BoltProposalMsg may be delivered later
		//	return nil
		//}
		//
		//blockBytes, err := encode(cBlk.Block)
		//if err != nil {
		//	b.bLogger.Error("fail to encode the block", "block_index", height)
		//	return err
		//}
		//proof := sign_tools.AssembleIntactTSPartial(shares, b.node.PubKeyTS, blockBytes, b.node.N-b.node.F, b.node.N)

		go func() {
			b.proofReady <- ProofData{
				//Proof:  proof,
				Proof:  shares,
				Height: height,
			}
		}()
	}
	return nil
}

func (b *Bolt) handlePaceSyncMessage(t *PaceSyncMsg) {
	b.Lock()
	defer b.Unlock()
	b.paceSyncMsgsReceived[t.Sender] = struct{}{}
	if len(b.paceSyncMsgsReceived) >= b.node.F+1 && !b.paceSyncMsgSent {
		b.paceSyncMsgSent = true
		b.bLogger.Info("Broadcast a timeout message")
		go b.node.PlainBroadcast(PaceSyncMsgTag, PaceSyncMsg{SN: t.SN, Sender: b.node.Id, Epoch: b.node.Bolt.maxProofedHeight}, nil)
	}

	if len(b.paceSyncMsgsReceived) == 2*b.node.F+1 && b.node.status == 0 {
		b.bLogger.Info("Switch from Bolt to ABA after timeout being triggered")
		go func() {
			b.node.statusChangeSignal <- StatusChangeSignal{
				SN:     t.SN,
				Status: (b.node.status + 1) % STATUSCOUNT,
			}
		}()
	}
}

func (b *Bolt) TriggerPaceSync() {
	b.Lock()
	defer b.Unlock()
	if !b.paceSyncMsgSent {
		b.paceSyncMsgSent = true
		b.bLogger.Info("Broadcast a pace sync message")
		go b.node.PlainBroadcast(PaceSyncMsgTag, PaceSyncMsg{SN: b.node.sn, Sender: b.node.Id, Epoch: b.maxProofedHeight}, nil)
	}
}
