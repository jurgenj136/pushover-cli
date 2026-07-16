package resolver

import (
	"bytes"
	"testing"
)

func FuzzCredentialsJSON(f *testing.F) {
	f.Add([]byte(`{"token":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaa","user_key":"bbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"}`))
	f.Add([]byte(`{}`))
	f.Add([]byte{0xff})
	f.Fuzz(func(t *testing.T, input []byte) {
		if len(input) > maxCredentialsBytes+32 {
			return
		}
		_, _ = parseCredentialsJSON(bytes.NewReader(input))
	})
}
