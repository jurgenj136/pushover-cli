// Package e2ee implements Pushover-compatible field encryption.
package e2ee

import (
	"encoding/hex"
	"errors"
)

var errInvalidEncryptionKey = errors.New("invalid encryption key")

// ParseKey decodes Pushover's exact 64-character hexadecimal key format.
// Its error intentionally does not disclose malformed input.
func ParseKey(value string) ([32]byte, error) {
	return parseKeyBytes([]byte(value))
}

func parseKeyBytes(value []byte) ([32]byte, error) {
	var key [32]byte
	if len(value) != 64 {
		return key, errInvalidEncryptionKey
	}
	if _, err := hex.Decode(key[:], value); err != nil {
		clear(key[:])
		return [32]byte{}, errInvalidEncryptionKey
	}
	return key, nil
}
