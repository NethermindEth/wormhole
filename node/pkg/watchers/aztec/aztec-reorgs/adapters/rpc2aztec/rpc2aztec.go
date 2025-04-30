package rpc2aztec

import (
	"time"

	"github.com/certusone/wormhole/node/pkg/watchers/aztec/aztec-reorgs/aztec"
	"github.com/certusone/wormhole/node/pkg/watchers/aztec/aztec-reorgs/rpc"
	"github.com/certusone/wormhole/node/pkg/watchers/aztec/aztec-reorgs/utils"
	"github.com/consensys/gnark-crypto/ecc/grumpkin/fp"
)

func Tip(t rpc.Tip) aztec.Tip {
	return aztec.Tip{
		Number: t.Number,
		Hash:   fp.Element(t.Hash),
	}
}

func L2Tips(ts rpc.L2Tips) aztec.L2Tips {
	return aztec.L2Tips{
		Latest:    Tip(ts.Latest),
		Proven:    Tip(ts.Proven),
		Finalized: Tip(ts.Finalized),
	}
}

func AppendOnlyTreeSnapshot(s rpc.AppendOnlyTreeSnapshot) aztec.AppendOnlyTreeSnapshot {
	return aztec.AppendOnlyTreeSnapshot{
		Root:                   fp.Element(s.Root),
		NextAvailableLeafIndex: s.NextAvailableLeafIndex,
	}
}

func ContentCommitment(c rpc.ContentCommitment) aztec.ContentCommitment {
	return aztec.ContentCommitment{
		NumTxs:    fp.Element(c.NumTxs),
		BlobsHash: fp.Element(c.BlobsHash),
		InHash:    fp.Element(c.InHash),
		OutHash:   fp.Element(c.OutHash),
	}
}

func PartialStateReference(s rpc.PartialStateReference) aztec.PartialStateReference {
	return aztec.PartialStateReference{
		NoteHashTree:   AppendOnlyTreeSnapshot(s.NoteHashTree),
		NullifierTree:  AppendOnlyTreeSnapshot(s.NullifierTree),
		PublicDataTree: AppendOnlyTreeSnapshot(s.PublicDataTree),
	}
}

func StateReference(s rpc.StateReference) aztec.StateReference {
	return aztec.StateReference{
		L1ToL2MsgTree:         AppendOnlyTreeSnapshot(s.L1ToL2MsgTree),
		PartialStateReference: PartialStateReference(s.PartialStateReference),
	}
}

func GasFees(f rpc.GasFees) aztec.GasFees {
	return aztec.GasFees{
		FeePerDAGas: fp.Element(f.FeePerDAGas),
		FeePerL2Gas: fp.Element(f.FeePerL2Gas),
	}
}

func GlobalVariables(v rpc.GlobalVariables) aztec.GlobalVariables {
	blockNumber := fp.Element(v.BlockNumber)
	slotNumber := fp.Element(v.SlotNumber)
	return aztec.GlobalVariables{
		ChainID:      fp.Element(v.ChainID),
		Version:      fp.Element(v.Version),
		BlockNumber:  blockNumber.Uint64(),
		SlotNumber:   slotNumber.Uint64(),
		Timestamp:    time.Time(v.Timestamp),
		Coinbase:     v.Coinbase,
		FeeRecipient: fp.Element(v.FeeRecipient),
		GasFees:      GasFees(v.GasFees),
	}
}

func Header(h rpc.Header) aztec.Header {
	return aztec.Header{
		LastArchive:       AppendOnlyTreeSnapshot(h.LastArchive),
		ContentCommitment: ContentCommitment(h.ContentCommitment),
		StateReference:    StateReference(h.StateReference),
		GlobalVariables:   GlobalVariables(h.GlobalVariables),
		TotalFees:         fp.Element(h.TotalFees),
		TotalManaUsed:     fp.Element(h.TotalManaUsed),
	}
}

func hexFpToFp(h rpc.HexFp) fp.Element {
	return fp.Element(h)
}

func PublicDataWrite(w rpc.PublicDataWrite) aztec.PublicDataWrite {
	return aztec.PublicDataWrite{
		LeafSlot: fp.Element(w.LeafSlot),
		Value:    fp.Element(w.Value),
	}
}

func PrivateLogData(d rpc.PrivateLogData) aztec.PrivateLogData {
	return aztec.PrivateLogData{
		Log:             aztec.PrivateLog(utils.Map(d.Log, hexFpToFp)),
		NoteHashCounter: uint32(d.NoteHashCounter),
		Counter:         uint32(d.Counter),
	}
}

func ContractClassLog(l rpc.ContractClassLogData) aztec.ContractClassLog {
	return aztec.ContractClassLog{
		ContractAddress: fp.Element(l.ContractAddress),
		Log:             utils.Map(l.Log, hexFpToFp),
	}
}

func PublicLog(l rpc.PublicLog) aztec.PublicLog {
	return utils.Map(l, hexFpToFp)
}

func TxEffect(e rpc.TxEffect) aztec.TxEffect {
	return aztec.TxEffect{
		TxHash:            fp.Element(e.TxHash),
		RevertCode:        aztec.RevertCode(e.RevertCode),
		TxFee:             fp.Element(e.TxFee),
		NoteHashes:        utils.Map(e.NoteHashes, hexFpToFp),
		Nullifiers:        utils.Map(e.Nullifiers, hexFpToFp),
		L2ToL1MsgHashes:   utils.Map(e.L2ToL1MsgHashes, hexFpToFp),
		PublicDataWrites:  utils.Map(e.PublicDataWrites, PublicDataWrite),
		PrivateLogs:       utils.Map(e.PrivateLogs, PrivateLogData),
		PublicLogs:        utils.Map(e.PublicLogs, PublicLog),
		ContractClassLogs: utils.Map(e.ContractClassLogs, ContractClassLog),
	}
}

func Body(b rpc.Body) aztec.Body {
	return aztec.Body{
		TxEffects: utils.Map(b.TxEffects, TxEffect),
	}
}

func Block(b rpc.Block) aztec.Block {
	return aztec.Block{
		Archive: AppendOnlyTreeSnapshot(b.Archive),
		Header:  Header(b.Header),
		Body:    Body(b.Body),
	}
}
