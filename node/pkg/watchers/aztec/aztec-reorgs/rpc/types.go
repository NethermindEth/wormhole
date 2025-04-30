package rpc

import (
	"encoding/base64"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"math/big"
	"time"

	"github.com/certusone/wormhole/node/pkg/watchers/aztec/aztec-reorgs/constants"
	"github.com/consensys/gnark-crypto/ecc/grumpkin/fp"
	"github.com/ethereum/go-ethereum/common"
)

// modulus is a package-global variable and must not be mutated.
var modulus *big.Int

func init() {
	modulus = fp.Modulus()
}

type hexBytes []byte

func (h hexBytes) String() string {
	// TODO padding. How to make sure it aligns with MarshalJSON?
	return "0x" + hex.EncodeToString(h)
}

func (h hexBytes) MarshalJSON() ([]byte, error) {
	// TODO padding.
	return append(hex.AppendEncode([]byte(`"0x`), h), '"'), nil
}

func (h *hexBytes) UnmarshalJSON(data []byte) error {
	if len(data) < 4 {
		return errors.New(`expected data to be of least length 4, as in "0x"`)
	}
	if got := data[0]; got != '"' {
		return fmt.Errorf(`expected first character of hex string to be '"', but got %c`, got)
	} else if got = data[len(data)-1]; got != '"' {
		return fmt.Errorf(`expected last character of hex string to be '"', but got %c`, got)
	} else if gotPrefix := string(data[1:3]); gotPrefix != "0x" {
		return fmt.Errorf("expected hex string to have the 0x prefix, but got: %s", gotPrefix)
	}
	out, err := hex.AppendDecode(nil, data[3:len(data)-1])
	if err != nil {
		return fmt.Errorf("decode hex: %v", err)
	}
	*h = out // Only modify h when AppendDecode is successful.
	return nil
}

type HexFp fp.Element

func (h HexFp) String() string {
	x := fp.Element(h)
	xBytes := x.Bytes()
	return hexBytes(xBytes[:]).String()
}

func (h HexFp) MarshalJSON() ([]byte, error) {
	// TODO see if we can get the Bytes() api changed to not use a pointer.
	x := fp.Element(h)
	xBytes := x.Bytes()
	return json.Marshal(hexBytes(xBytes[:]))
}

func (h *HexFp) UnmarshalJSON(data []byte) error {
	var x hexBytes
	if err := json.Unmarshal(data, &x); err != nil {
		return err
	}
	fpBig, err := bytesToBigFp([]byte(x))
	if err != nil {
		return err
	}
	*h = HexFp(*new(fp.Element).SetBigInt(fpBig))
	return nil
}

type HexTime time.Time

func (h HexTime) MarshalJSON() ([]byte, error) {
	out := make([]byte, 8)
	binary.BigEndian.PutUint64(out, uint64(time.Time(h).Unix()))
	outJSON, err := json.Marshal(hexBytes(out))
	if err != nil {
		return nil, err
	}
	return outJSON, nil
}

func (h *HexTime) UnmarshalJSON(data []byte) error {
	var x hexBytes
	if err := json.Unmarshal(data, &x); err != nil {
		return err
	}
	*h = HexTime(time.Unix(int64(binary.BigEndian.Uint64([]byte(x))), 0))
	return nil
}

type HexUint32 uint32

func (h HexUint32) MarshalJSON() ([]byte, error) {
	outUint32 := make([]byte, 4)
	binary.BigEndian.PutUint32(outUint32, uint32(h))
	out, err := json.Marshal(hexBytes(outUint32))
	if err != nil {
		return nil, err
	}
	return out, nil
}

func (h *HexUint32) UnmarshalJSON(data []byte) error {
	var x hexBytes
	if err := json.Unmarshal(data, &x); err != nil {
		return err
	}
	*h = HexUint32(binary.BigEndian.Uint32(x))
	return nil
}

type Tip struct {
	Number uint64 `json:"number"`
	Hash   HexFp  `json:"hash"`
}

type L2Tips struct {
	Latest    Tip `json:"latest"`
	Proven    Tip `json:"proven"`
	Finalized Tip `json:"finalized"`
}

type Base64Fp fp.Element

func (b Base64Fp) MarshalJSON() ([]byte, error) {
	out := []byte{'"'}
	bFp := fp.Element(b)
	bFpBytes := bFp.Bytes()
	base64.StdEncoding.AppendEncode(out, bFpBytes[:])
	return append(out, '"'), nil
}

func (b *Base64Fp) UnmarshalJSON(data []byte) error {
	if len(data) < 2 {
		return fmt.Errorf("expected data to be of least length 2 for quotation marks, got length %d: %s", len(data), data)
	}
	if got := data[0]; got != '"' {
		return fmt.Errorf(`expected first character of base64 string to be '"', but got %c`, got)
	} else if got = data[len(data)-1]; got != '"' {
		return fmt.Errorf(`expected last character of base64 string to be '"', but got %c`, got)
	}
	var fpBytes []byte
	fpBytes, err := base64.StdEncoding.AppendDecode(fpBytes, data[1:len(data)-1])
	if err != nil {
		return fmt.Errorf("decode base64: %v", err)
	}
	fpBig, err := bytesToBigFp(fpBytes)
	if err != nil {
		return err
	}
	*b = Base64Fp(*new(fp.Element).SetBigInt(fpBig)) // Only modify b when AppendDecode is successful.
	return nil
}

func bytesToBigFp(b []byte) (*big.Int, error) {
	fpBig := new(big.Int).SetBytes(b)
	if fpBig.Cmp(modulus) != -1 || fpBig.Sign() == -1 {
		return nil, fmt.Errorf("element is not a member of the field: %s", "0x"+fpBig.Text(16))
	}
	return fpBig, nil
}

type AppendOnlyTreeSnapshot struct {
	Root                   HexFp  `json:"root"`
	NextAvailableLeafIndex uint32 `json:"nextAvailableLeafIndex"`
}

type ContentCommitment struct {
	NumTxs    HexFp    `json:"numTxs"`
	BlobsHash Base64Fp `json:"blobsHash"`
	InHash    Base64Fp `json:"inHash"`
	OutHash   Base64Fp `json:"outHash"`
}

type PartialStateReference struct {
	NoteHashTree   AppendOnlyTreeSnapshot `json:"noteHashTree"`
	NullifierTree  AppendOnlyTreeSnapshot `json:"nullifierTree"`
	PublicDataTree AppendOnlyTreeSnapshot `json:"publicDataTree"`
}

type GasFees struct {
	FeePerDAGas HexFp `json:"feePerDaGas"`
	FeePerL2Gas HexFp `json:"feePerL2Gas"`
}

type GlobalVariables struct {
	ChainID      HexFp          `json:"chainId"`
	Version      HexFp          `json:"version"`
	BlockNumber  HexFp          `json:"blockNumber"`
	SlotNumber   HexFp          `json:"slotNumber"`
	Timestamp    HexTime        `json:"timestamp"`
	Coinbase     common.Address `json:"coinbase"`
	FeeRecipient HexFp          `json:"feeRecipient"`
	GasFees      GasFees        `json:"gasFees"`
}

type StateReference struct {
	L1ToL2MsgTree         AppendOnlyTreeSnapshot `json:"l1ToL2MessageTree"`
	PartialStateReference PartialStateReference  `json:"partial"`
}

type Header struct {
	LastArchive       AppendOnlyTreeSnapshot `json:"lastArchive"`
	ContentCommitment ContentCommitment      `json:"contentCommitment"`
	StateReference    StateReference         `json:"state"`
	GlobalVariables   GlobalVariables        `json:"globalVariables"`
	TotalFees         HexFp                  `json:"totalFees"`
	TotalManaUsed     HexFp                  `json:"totalManaUsed"`
}

type RevertCode byte

const (
	RevertCodeOk RevertCode = iota
	RevertCodeAppLogic
	RevertCodeTeardown
	RevertCodeBoth
	RevertCodeInvalid // Not an actual revert code.
)

func (rc *RevertCode) UnmarshalJSON(data []byte) error {
	if length := len(data); length != 1 {
		return fmt.Errorf("expected data to have length 1, got %d: %s", length, data)
	}
	var got byte
	if err := json.Unmarshal(data, &got); err != nil {
		return err
	}
	if got >= byte(RevertCodeInvalid) {
		return fmt.Errorf("invalid revert code: %d", got)
	}
	*rc = RevertCode(got)
	return nil
}

type PublicDataWrite struct {
	LeafSlot HexFp `json:"leafSlot"`
	Value    HexFp `json:"value"`
}

type PrivateLog []HexFp

func (x PrivateLog) MarshalJSON() ([]byte, error) {
	return marshalHexFpSliceWithMaxLength(x, constants.PrivateLogSizeInFields)
}

func (x *PrivateLog) UnmarshalJSON(data []byte) error {
	got, err := unmarshalHexFpSliceWithMaxLength[HexFp](data, constants.PrivateLogSizeInFields)
	if err != nil {
		return err
	}
	*x = got
	return nil
}

type PrivateLogData struct {
	Log             PrivateLog `json:"fields"`
	NoteHashCounter HexUint32  `json:"noteHashCounter,omitempty"`
	Counter         HexUint32  `json:"counter,omitempty"`
}

type ContractClassLog []HexFp

func (x ContractClassLog) MarshalJSON() ([]byte, error) {
	return marshalHexFpSliceWithMaxLength(x, constants.ContractClassLogDataSizeInFields)
}

func (x *ContractClassLog) UnmarshalJSON(data []byte) error {
	got, err := unmarshalHexFpSliceWithMaxLength[HexFp](data, constants.ContractClassLogDataSizeInFields)
	if err != nil {
		return err
	}
	*x = got
	return nil
}

type ContractClassLogData struct {
	ContractAddress HexFp            `json:"contractAddress"`
	Log             ContractClassLog `json:"fields"`
}

func marshalHexFpSliceWithMaxLength[T any](s []T, max int) ([]byte, error) {
	if length := len(s); length > max {
		return nil, fmt.Errorf("slice is too long: max length is %d, got %d", max, length)
	}
	return json.Marshal(s)
}

func unmarshalHexFpSliceWithMaxLength[T any](data []byte, max int) ([]T, error) {
	var out []T
	if err := json.Unmarshal(data, &out); err != nil {
		return nil, err
	}
	if length := len(out); length > max {
		return nil, fmt.Errorf("slice is too long: max length is %d, got %d", max, length)
	}
	return out, nil
}

type NoteHashes []HexFp

func (x NoteHashes) MarshalJSON() ([]byte, error) {
	return marshalHexFpSliceWithMaxLength(x, constants.MaxNoteHashesPerTx)
}

func (x *NoteHashes) UnmarshalJSON(data []byte) error {
	got, err := unmarshalHexFpSliceWithMaxLength[HexFp](data, constants.MaxNoteHashesPerTx)
	if err != nil {
		return err
	}
	*x = got
	return nil
}

type Nullifiers []HexFp

func (x Nullifiers) MarshalJSON() ([]byte, error) {
	return marshalHexFpSliceWithMaxLength(x, constants.MaxNullifiersPerTx)
}

func (x *Nullifiers) UnmarshalJSON(data []byte) error {
	got, err := unmarshalHexFpSliceWithMaxLength[HexFp](data, constants.MaxNullifiersPerTx)
	if err != nil {
		return err
	}
	*x = got
	return nil
}

type L2ToL1MsgHashes []HexFp

func (x L2ToL1MsgHashes) MarshalJSON() ([]byte, error) {
	return marshalHexFpSliceWithMaxLength(x, constants.MaxL2ToL1MsgsPerTx)
}

func (x *L2ToL1MsgHashes) UnmarshalJSON(data []byte) error {
	got, err := unmarshalHexFpSliceWithMaxLength[HexFp](data, constants.MaxL2ToL1MsgsPerTx)
	if err != nil {
		return err
	}
	*x = got
	return nil
}

type PublicDataWrites []PublicDataWrite

func (x PublicDataWrites) MarshalJSON() ([]byte, error) {
	return marshalHexFpSliceWithMaxLength(x, constants.MaxTotalPublicDataUpdateRequestsPerTx)
}

func (x *PublicDataWrites) UnmarshalJSON(data []byte) error {
	got, err := unmarshalHexFpSliceWithMaxLength[PublicDataWrite](data, constants.MaxTotalPublicDataUpdateRequestsPerTx)
	if err != nil {
		return err
	}
	*x = got
	return nil
}

type PrivateLogDatas []PrivateLogData

func (x PrivateLogDatas) MarshalJSON() ([]byte, error) {
	return marshalHexFpSliceWithMaxLength(x, constants.MaxPrivateLogsPerTx)
}

func (x *PrivateLogDatas) UnmarshalJSON(data []byte) error {
	got, err := unmarshalHexFpSliceWithMaxLength[PrivateLogData](data, constants.MaxPrivateLogsPerTx)
	if err != nil {
		return err
	}
	*x = got
	return nil
}

type PublicLog []HexFp

func (x PublicLog) MarshalJSON() ([]byte, error) {
	return marshalHexFpSliceWithMaxLength(x, constants.PublicLogDataSizeInFields)
}

func (x *PublicLog) UnmarshalJSON(data []byte) error {
	got, err := unmarshalHexFpSliceWithMaxLength[HexFp](data, constants.PublicLogDataSizeInFields)
	if err != nil {
		return err
	}
	*x = got
	return nil
}

type PublicLogs []PublicLog

func (x PublicLogs) MarshalJSON() ([]byte, error) {
	return marshalHexFpSliceWithMaxLength(x, constants.MaxPublicLogsPerTx)
}

func (x *PublicLogs) UnmarshalJSON(data []byte) error {
	got, err := unmarshalHexFpSliceWithMaxLength[PublicLog](data, constants.MaxPublicLogsPerTx)
	if err != nil {
		return err
	}
	*x = got
	return nil
}

type ContractClassLogDatas []ContractClassLogData

func (x ContractClassLogDatas) MarshalJSON() ([]byte, error) {
	return marshalHexFpSliceWithMaxLength(x, constants.MaxContractClassLogsPerTx)
}

func (x *ContractClassLogDatas) UnmarshalJSON(data []byte) error {
	got, err := unmarshalHexFpSliceWithMaxLength[ContractClassLogData](data, constants.MaxContractClassLogsPerTx)
	if err != nil {
		return err
	}
	*x = got
	return nil
}

type TxEffect struct {
	TxHash            HexFp                 `json:"txHash"`
	RevertCode        RevertCode            `json:"revertCode"`
	TxFee             HexFp                 `json:"transactionFee"`
	NoteHashes        NoteHashes            `json:"noteHashes"`
	Nullifiers        Nullifiers            `json:"nullifiers"`
	L2ToL1MsgHashes   L2ToL1MsgHashes       `json:"l2ToL1Messages,omitempty"`
	PublicDataWrites  PublicDataWrites      `json:"publicDataWrites"`
	PrivateLogs       PrivateLogDatas       `json:"privateLogs"`
	PublicLogs        PublicLogs            `json:"publicLogs"`
	ContractClassLogs ContractClassLogDatas `json:"contractClassLogs"`
}

type Body struct {
	TxEffects []TxEffect `json:"txEffects"`
}

type Block struct {
	Archive AppendOnlyTreeSnapshot `json:"archive"`
	Header  Header                 `json:"header"`
	Body    Body                   `json:"body"`
}
