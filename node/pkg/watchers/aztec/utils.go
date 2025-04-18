package aztec

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
)

// Helper functions for common parsing tasks

// ParseInt parses a string as an integer with proper error handling
func ParseInt(s string, base int, bitSize int) (int64, error) {
	v, err := strconv.ParseInt(s, base, bitSize)
	if err != nil {
		return 0, &ErrParsingFailed{
			What: fmt.Sprintf("integer with base %d", base),
			Err:  err,
		}
	}
	return v, nil
}

// ParseUint parses a string as an unsigned integer with proper error handling
func ParseUint(s string, base int, bitSize int) (uint64, error) {
	v, err := strconv.ParseUint(s, base, bitSize)
	if err != nil {
		return 0, &ErrParsingFailed{
			What: fmt.Sprintf("unsigned integer with base %d", base),
			Err:  err,
		}
	}
	return v, nil
}

// ParseHexUint64 converts a hex string to uint64
func ParseHexUint64(hexStr string) (uint64, error) {
	// Remove "0x" prefix if present
	hexStr = strings.TrimPrefix(hexStr, "0x")

	// Parse the hex string to uint64
	value, err := strconv.ParseUint(hexStr, 16, 64)
	if err != nil {
		return 0, &ErrParsingFailed{
			What: "hex uint64",
			Err:  err,
		}
	}

	return value, nil
}

// IsPrintableString checks if a string contains mostly printable ASCII characters
func IsPrintableString(s string) bool {
	printable := 0
	for _, r := range s {
		if r >= 32 && r <= 126 {
			printable++
		}
	}
	return printable >= 3 && float64(printable)/float64(len(s)) > 0.5
}

// GetJSONRPCError extracts error information from a JSON-RPC response
func GetJSONRPCError(body []byte) (bool, *ErrRPCError) {
	var errorCheck struct {
		Error *struct {
			Code    int    `json:"code"`
			Message string `json:"message"`
		} `json:"error,omitempty"`
	}

	if err := json.Unmarshal(body, &errorCheck); err != nil || errorCheck.Error == nil {
		return false, nil
	}

	return true, &ErrRPCError{
		Method: "unknown", // Caller should update this
		Code:   errorCheck.Error.Code,
		Msg:    errorCheck.Error.Message,
	}
}

// IsRetryableRPCError determines if an RPC error should be retried
// Generally server errors (code < -32000) are retryable
func IsRetryableRPCError(err *ErrRPCError) bool {
	return err.Code >= -32099 && err.Code <= -32000
}

// CreateObservationID creates a unique ID for tracking pending observations
func CreateObservationID(senderAddress string, sequence uint64, blockNumber int) string {
	return fmt.Sprintf("%s-%d-%d", senderAddress, sequence, blockNumber)
}
