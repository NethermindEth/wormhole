package aztec

import (
	"slices"
	"time"

	"github.com/certusone/wormhole/node/pkg/watchers/aztec/aztec-reorgs/constants"
	"github.com/consensys/gnark-crypto/ecc/grumpkin/fp"
	"github.com/consensys/gnark-crypto/ecc/grumpkin/fr/poseidon2"
	"github.com/ethereum/go-ethereum/common"
)

type TxEffect struct {
	TxHash            fp.Element
	RevertCode        RevertCode
	TxFee             fp.Element
	NoteHashes        []fp.Element
	Nullifiers        []fp.Element
	L2ToL1MsgHashes   []fp.Element
	PublicDataWrites  []PublicDataWrite
	PrivateLogs       []PrivateLogData
	PublicLogs        [][]fp.Element
	ContractClassLogs []ContractClassLog
}

type Body struct {
	TxEffects []TxEffect
}

type Block struct {
	Archive AppendOnlyTreeSnapshot
	Header  Header
	Body    Body
}

func (b Block) Equal(other Block) bool {
	return b.Header.Equal(other.Header)
}

type GlobalVariables struct {
	ChainID      fp.Element
	Version      fp.Element
	BlockNumber  uint64
	SlotNumber   uint64
	Timestamp    time.Time
	Coinbase     common.Address
	FeeRecipient fp.Element
	GasFees      GasFees
}

func (g GlobalVariables) ToFields() []fp.Element {
	return slices.Concat([]fp.Element{
		g.ChainID,
		g.Version,
		*new(fp.Element).SetUint64(g.BlockNumber),
		*new(fp.Element).SetUint64(g.SlotNumber),
		*new(fp.Element).SetInt64(g.Timestamp.Unix()),
		*new(fp.Element).SetBytes(g.Coinbase.Bytes()),
		g.FeeRecipient,
	}, g.GasFees.ToFields())
}

type StateReference struct {
	L1ToL2MsgTree         AppendOnlyTreeSnapshot
	PartialStateReference PartialStateReference
}

func (s StateReference) ToFields() []fp.Element {
	return slices.Concat(s.L1ToL2MsgTree.ToFields(), s.PartialStateReference.ToFields())
}

type Header struct {
	LastArchive       AppendOnlyTreeSnapshot
	ContentCommitment ContentCommitment
	StateReference    StateReference
	GlobalVariables   GlobalVariables
	TotalFees         fp.Element
	TotalManaUsed     fp.Element
	// CachedHash is a hash from a cached value, which is not guaranteed to be populated.
	// Use Header.Hash() to get the hash.
	CachedHash *fp.Element
}

func (h Header) ToFields() []fp.Element {
	return slices.Concat(
		h.LastArchive.ToFields(),
		h.ContentCommitment.ToFields(),
		h.StateReference.ToFields(),
		h.GlobalVariables.ToFields(),
		[]fp.Element{h.TotalFees, h.TotalManaUsed},
	)
}

func (h Header) ToTip() Tip {
	return Tip{
		Number: h.GlobalVariables.BlockNumber,
		Hash:   h.Hash(),
	}
}

func (h Header) Equal(other Header) bool {
	hHash := h.Hash()
	otherHash := other.Hash()
	return hHash.Equal(&otherHash)
}

func (h Header) Hash() fp.Element {
	if h.CachedHash == nil {
		// things that can go wrong
		//   - we are using fp instead of fr
		//   - merkle damgard is not used in aztec
		//   - aztec does not use defeault parameters
		//   - poseidon2 implementation from gnark is wrong
		//   - fields are wrong order
		//   - marshalbinary is wrong
		//   - the setstate <> sum pattern is wrong
		state := fp.Vector(slices.Concat([]fp.Element{*new(fp.Element).SetUint64(uint64(constants.GeneratorIndexBlockHash))}, h.ToFields()))
		stateBytes, _ := state.MarshalBinary() // Always returns a nil error.
		hasher := poseidon2.NewMerkleDamgardHasher()
		_ = hasher.SetState(stateBytes) // Always returns nil for poseidon2.
		h.CachedHash = new(fp.Element).SetBytes(hasher.Sum(nil))
	}
	return *h.CachedHash
}

type PublicDataWrite struct {
	LeafSlot fp.Element
	Value    fp.Element
}

type PrivateLogData struct {
	Log             []fp.Element
	NoteHashCounter uint32
	Counter         uint32
}

type ContractClassLog struct {
	ContractAddress fp.Element
	Log             []fp.Element
}

type RevertCode byte

const (
	RevertCodeOk RevertCode = iota
	RevertCodeAppLogic
	RevertCodeTeardown
	RevertCodeBoth
	RevertCodeInvalid // Not an actual revert code.
)

func ToRevertCode(c byte) (RevertCode, bool) {
	if c >= byte(RevertCodeInvalid) {
		return 0, false
	}
	return RevertCode(c), true
}

type Tip struct {
	Number uint64
	Hash   fp.Element
}

type L2Tips struct {
	Latest    Tip
	Proven    Tip
	Finalized Tip
}

type AppendOnlyTreeSnapshot struct {
	Root                   fp.Element
	NextAvailableLeafIndex uint32
}

func (a AppendOnlyTreeSnapshot) ToFields() []fp.Element {
	return []fp.Element{a.Root, *new(fp.Element).SetUint64(uint64(a.NextAvailableLeafIndex))}
}

type ContentCommitment struct {
	NumTxs    fp.Element
	BlobsHash fp.Element
	InHash    fp.Element
	OutHash   fp.Element
}

func (c ContentCommitment) ToFields() []fp.Element {
	return []fp.Element{c.NumTxs, c.BlobsHash, c.InHash, c.OutHash}
}

type PartialStateReference struct {
	NoteHashTree   AppendOnlyTreeSnapshot
	NullifierTree  AppendOnlyTreeSnapshot
	PublicDataTree AppendOnlyTreeSnapshot
}

func (p PartialStateReference) ToFields() []fp.Element {
	return slices.Concat(p.NoteHashTree.ToFields(), p.NullifierTree.ToFields(), p.PublicDataTree.ToFields())
}

type GasFees struct {
	FeePerDAGas fp.Element
	FeePerL2Gas fp.Element
}

func (g GasFees) ToFields() []fp.Element {
	return []fp.Element{g.FeePerDAGas, g.FeePerL2Gas}
}
