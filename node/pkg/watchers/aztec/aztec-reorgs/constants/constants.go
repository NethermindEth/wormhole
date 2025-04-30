package constants

const (
	NoteHashSubtreeHeight   = 6
	NullifierSubtreeHeight  = 6
	PublicDataSubtreeHeight = 6

	MaxNoteHashesPerTx                    = 1 << NoteHashSubtreeHeight
	MaxNullifiersPerTx                    = 1 << NullifierSubtreeHeight
	MaxL2ToL1MsgsPerTx                    = 8
	MaxTotalPublicDataUpdateRequestsPerTx = 1 << PublicDataSubtreeHeight
	MaxPrivateLogsPerTx                   = 32
	MaxPublicLogsPerTx                    = 8
	MaxContractClassLogsPerTx             = 1

	MaxPackedBytecodeSizePerPrivateFunctionInFields      = 3000
	RegistererPrivateFunctionBroadcastedAdditionalFields = 19

	PrivateLogSizeInFields           = 18
	PublicLogDataSizeInFields        = 13
	ContractClassLogDataSizeInFields = MaxPackedBytecodeSizePerPrivateFunctionInFields + RegistererPrivateFunctionBroadcastedAdditionalFields
)

type GeneratorIndex byte

const (
	GeneratorIndexBlockHash GeneratorIndex = 28
)
