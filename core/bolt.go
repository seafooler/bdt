package core

import (
	"github.com/hashicorp/go-hclog"
	"github.com/seafooler/sign_tools"
	"sync"
)

type Bolt struct {
	node *Node

	bLogger  hclog.Logger
	leaderId int

	committedHeight uint32
	proofedHeight   map[uint32]bool
	cachedHeight    map[uint32]bool
	cachedVoteMsgs  map[uint32]map[int][]byte
	cachedBlocks    map[uint32]*Block

	proofReady chan ProofData
	sync.Mutex
}

func NewBolt(node *Node, leader int) *Bolt {
	return &Bolt{
		node: node,
		bLogger: hclog.New(&hclog.LoggerOptions{
			Name:   "bdt-bolt",
			Output: hclog.DefaultOutput,
			Level:  hclog.Level(node.Config.LogLevel),
		}),
		leaderId:        leader,
		committedHeight: 0,
		proofedHeight:   make(map[uint32]bool),
		cachedHeight:    make(map[uint32]bool),
		cachedVoteMsgs:  make(map[uint32]map[int][]byte),
		cachedBlocks:    make(map[uint32]*Block),
		proofReady:      make(chan ProofData),
	}
}

func (b *Bolt) ProposalLoop() {
	if b.node.Id != b.leaderId {
		return
	}

	for {
		proofReady := <-b.proofReady
		newBlock := &Block{
			Reqs:     nil,
			Height:   proofReady.Height + 1,
			Proposer: b.node.Id,
		}
		if err := b.BroadcastProposalProof(newBlock, proofReady.Proof); err != nil {
			b.bLogger.Error("fail to broadcast proposal and proof", "height", newBlock.Height,
				"err", err.Error())
		}
	}
}

// BroadcastProposalProof broadcasts the new block and proof of previous block through the ProposalMsg
func (b *Bolt) BroadcastProposalProof(blk *Block, proof []byte) error {
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
	// do not retrieve the previous block nor verify the proof for the first block
	if pm.Height == 0 {
		b.Lock()
		b.cachedHeight[pm.Height] = true
		b.cachedBlocks[pm.Height] = &pm.Block
		b.Unlock()
		return nil
	}

	// retrieve the previous block
	pBlk, ok := b.cachedBlocks[pm.Height-1]
	if !ok {
		// Todo: deal with the situation where the previous block has not been cached
		b.bLogger.Error("did not cache the previous block", "prev_block_index", pm.Height-1)
		return nil
	}

	// verify the proof
	if _, err := sign_tools.VerifyTS(b.node.PubKeyTS, pBlk.Reqs, pm.Proof); err != nil {
		b.bLogger.Error("fail to verify proof of a previous block", "prev_block_index", pBlk.Height)
		return err
	}

	b.Lock()
	b.proofedHeight[pBlk.Height] = true
	delete(b.cachedHeight, pBlk.Height)
	b.Unlock()

	// create the ts share of new block
	blockBytes, err := encode(pm.Block)
	if err != nil {
		b.bLogger.Error("fail to encode the block", "block_index", pm.Height)
		return err
	}
	share := sign_tools.SignTSPartial(b.node.PriKeyTS, blockBytes)

	// send the ts share to the leader
	boltVoteMsg := BoltVoteMsg{
		Share:  share,
		Height: pm.Height,
		Voter:  b.node.Id,
	}
	leaderAddrPort := b.node.Id2AddrMap[b.leaderId] + ":" + b.node.Id2PortMap[b.leaderId]
	err = b.node.SendMsg(BoltVoteMsgTag, boltVoteMsg, nil, leaderAddrPort)
	if err != nil {
		b.bLogger.Error("fail to vote for the block", "block_index", pm.Height)
		return err
	} else {
		b.bLogger.Debug("successfully vote for the block", "block_index", pm.Height)
	}

	// attempt to commit the prev-prev block
	b.Lock()
	defer b.Unlock()
	if _, ok := b.proofedHeight[pBlk.Height-1]; ok {
		b.bLogger.Info("commit the block", "block_index", pBlk.Height-1)
		// Todo: check the consecutive commitment
		b.committedHeight = pBlk.Height - 1
		delete(b.proofedHeight, pBlk.Height-1)
	}
	return nil
}

// ProcessBoltVoteMsg stores the vote messages and attempts to create the ts proof
func (b *Bolt) ProcessBoltVoteMsg(vm *BoltVoteMsg) error {
	b.Lock()
	defer b.Unlock()
	if _, ok := b.cachedVoteMsgs[vm.Height]; !ok {
		b.cachedVoteMsgs[vm.Height] = make(map[int][]byte)
	}
	b.cachedVoteMsgs[vm.Height][vm.Voter] = vm.Share

	if len(b.cachedVoteMsgs[vm.Height]) == b.node.N-b.node.F {
		shares := make([][]byte, b.node.N-b.node.F)
		i := 0
		for _, share := range b.cachedVoteMsgs[vm.Height] {
			shares[i] = share
			i++
		}
		blockBytes, err := encode(*b.cachedBlocks[vm.Height])
		if err != nil {
			b.bLogger.Error("fail to encode the block", "block_index", vm.Height)
			return err
		}
		proof := sign_tools.AssembleIntactTSPartial(shares, b.node.PubKeyTS, blockBytes, b.node.N-b.node.F, b.node.N)

		go func() {
			b.proofReady <- ProofData{
				Proof:  proof,
				Height: vm.Height,
			}
		}()
	}
	return nil
}
