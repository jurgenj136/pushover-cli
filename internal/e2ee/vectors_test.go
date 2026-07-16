package e2ee

import (
	"bytes"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"os"
	"testing"
)

type vector struct {
	Name, Key, IV, Plaintext, Envelope string
}

func TestIndependentVectors(t *testing.T) {
	raw, err := os.ReadFile("testdata/vectors.json")
	if err != nil {
		t.Fatal(err)
	}
	var vectors []vector
	if err := json.Unmarshal(raw, &vectors); err != nil {
		t.Fatal(err)
	}
	if len(vectors) == 0 {
		t.Fatal("no independent encryption vectors")
	}
	for _, test := range vectors {
		t.Run(test.Name, func(t *testing.T) {
			key, err := ParseKey(test.Key)
			if err != nil {
				t.Fatal(err)
			}
			if got := decryptEnvelope(t, key, test.Envelope); got != test.Plaintext {
				t.Fatalf("decrypted plaintext = %q, want %q", got, test.Plaintext)
			}
			envelope, err := base64.StdEncoding.DecodeString(test.Envelope)
			if err != nil {
				t.Fatal(err)
			}
			iv, err := hex.DecodeString(test.IV)
			if err != nil {
				t.Fatal(err)
			}
			if len(envelope) < len(iv) || !bytes.Equal(envelope[:len(iv)], iv) {
				t.Fatal("vector envelope does not begin with its declared IV")
			}
		})
	}
}
