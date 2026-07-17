package e2ee

import (
	"bytes"
	"compress/gzip"
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"io"
	"strings"
	"testing"

	"github.com/jurgenj136/pushover-cli/internal/domain"
)

const maxDecodedPlaintext = 64 * 1024

var errMalformedEnvelope = errors.New("malformed encrypted envelope")

func decryptEnvelope(t *testing.T, key [32]byte, value string) string {
	t.Helper()
	plaintext, err := decodeEnvelope(key, value)
	if err != nil {
		t.Fatalf("decodeEnvelope() error = %v", err)
	}
	return plaintext
}

// decodeEnvelope is test-only: it proves compatibility without becoming a
// production decryption capability.
func decodeEnvelope(key [32]byte, value string) (string, error) {
	envelope, err := base64.StdEncoding.DecodeString(value)
	if err != nil || len(envelope) < aes.BlockSize+aes.BlockSize+sha256.Size {
		return "", errMalformedEnvelope
	}
	defer clear(envelope)

	macStart := len(envelope) - sha256.Size
	ciphertext := envelope[aes.BlockSize:macStart]
	if len(ciphertext) == 0 || len(ciphertext)%aes.BlockSize != 0 {
		return "", errMalformedEnvelope
	}
	expected := hmac.New(sha256.New, key[:])
	_, _ = expected.Write(envelope[:macStart])
	if !hmac.Equal(expected.Sum(nil), envelope[macStart:]) {
		return "", errMalformedEnvelope
	}

	block, err := aes.NewCipher(key[:])
	if err != nil {
		return "", errMalformedEnvelope
	}
	padded := make([]byte, len(ciphertext))
	defer clear(padded)
	cipher.NewCBCDecrypter(block, envelope[:aes.BlockSize]).CryptBlocks(padded, ciphertext)
	compressed, ok := pkcs7Unpad(padded, aes.BlockSize)
	if !ok {
		return "", errMalformedEnvelope
	}
	return gunzipBounded(compressed)
}

func pkcs7Unpad(input []byte, blockSize int) ([]byte, bool) {
	if len(input) == 0 || len(input)%blockSize != 0 {
		return nil, false
	}
	padding := int(input[len(input)-1])
	if padding == 0 || padding > blockSize || padding > len(input) {
		return nil, false
	}
	var mismatch byte
	for _, value := range input[len(input)-padding:] {
		mismatch |= value ^ byte(padding)
	}
	return input[:len(input)-padding], mismatch == 0
}

func gunzipBounded(compressed []byte) (string, error) {
	reader, err := gzip.NewReader(bytes.NewReader(compressed))
	if err != nil {
		return "", errMalformedEnvelope
	}
	plaintext, err := io.ReadAll(io.LimitReader(reader, maxDecodedPlaintext+1))
	if err != nil || len(plaintext) > maxDecodedPlaintext {
		clear(plaintext)
		_ = reader.Close()
		return "", errMalformedEnvelope
	}
	if err := reader.Close(); err != nil {
		clear(plaintext)
		return "", errMalformedEnvelope
	}
	output := string(plaintext)
	clear(plaintext)
	return output, nil
}

func TestGzipBytesUsesDeterministicHeader(t *testing.T) {
	encoded, err := gzipBytes([]byte("payload"))
	if err != nil {
		t.Fatal(err)
	}
	if len(encoded) < 10 || !bytes.Equal(encoded[:4], []byte{0x1f, 0x8b, 0x08, 0x00}) ||
		!bytes.Equal(encoded[4:8], []byte{0, 0, 0, 0}) || encoded[8] != 2 || encoded[9] != 255 {
		t.Fatalf("gzip header = %x, want deterministic gzip -9 header", encoded[:min(len(encoded), 10)])
	}
}

func TestPKCS7PadRejectsInvalidBlockSizes(t *testing.T) {
	for _, test := range []struct {
		name string
		size int
	}{{name: "zero", size: 0}, {name: "too-large", size: 256}} {
		t.Run(test.name, func(t *testing.T) {
			defer func() {
				if recover() == nil {
					t.Fatal("invalid block size did not panic")
				}
			}()
			_ = pkcs7Pad(nil, test.size)
		})
	}
}

func TestDecodeEnvelopeRejectsMalformedAndTamperedInput(t *testing.T) {
	key, err := ParseKey(testKey)
	if err != nil {
		t.Fatal(err)
	}
	valid, err := (Encryptor{Random: bytes.NewReader(bytes.Repeat([]byte{0x4a}, aes.BlockSize))}).EncryptPayload(context.Background(), domain.TargetPayload{Target: domain.Target{E2EEKey: []byte(testKey)}, Message: domain.Message{Body: "body"}})
	if err != nil {
		t.Fatal(err)
	}
	encoded := valid.Message.Body
	raw, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		t.Fatal(err)
	}
	for _, index := range []int{0, aes.BlockSize, len(raw) - 1} {
		copy := append([]byte(nil), raw...)
		copy[index] ^= 1
		if _, err := decodeEnvelope(key, base64.StdEncoding.EncodeToString(copy)); !errors.Is(err, errMalformedEnvelope) {
			t.Errorf("tampered byte %d error = %v, want malformed-envelope error", index, err)
		}
	}

	badPadding := append(bytes.Repeat([]byte{0}, aes.BlockSize-1), 0)
	inconsistentPadding := append(bytes.Repeat([]byte{0}, aes.BlockSize-3), 1, 2, 3)
	badGzip := pkcs7Pad([]byte("not gzip"), aes.BlockSize)
	tooLarge, err := gzipBytes([]byte(strings.Repeat("x", maxDecodedPlaintext+1)))
	if err != nil {
		t.Fatal(err)
	}
	for _, encoded := range []string{
		"",
		"%%%",
		base64.StdEncoding.EncodeToString(make([]byte, aes.BlockSize+aes.BlockSize+sha256.Size-1)),
		base64.StdEncoding.EncodeToString(make([]byte, aes.BlockSize+aes.BlockSize+1+sha256.Size)),
		envelopeForTest(key, bytes.Repeat([]byte{0x12}, aes.BlockSize), badPadding),
		envelopeForTest(key, bytes.Repeat([]byte{0x12}, aes.BlockSize), inconsistentPadding),
		envelopeForTest(key, bytes.Repeat([]byte{0x12}, aes.BlockSize), badGzip),
		envelopeForTest(key, bytes.Repeat([]byte{0x12}, aes.BlockSize), pkcs7Pad(tooLarge, aes.BlockSize)),
	} {
		if _, err := decodeEnvelope(key, encoded); !errors.Is(err, errMalformedEnvelope) {
			t.Errorf("decodeEnvelope(%q) error = %v, want malformed-envelope error", encoded[:min(len(encoded), 20)], err)
		}
	}
	wrongKey := key
	wrongKey[0] ^= 1
	if _, err := decodeEnvelope(wrongKey, encoded); !errors.Is(err, errMalformedEnvelope) {
		t.Errorf("wrong-key decode error = %v, want malformed-envelope error", err)
	}
}

func envelopeForTest(key [32]byte, iv, padded []byte) string {
	block, err := aes.NewCipher(key[:])
	if err != nil {
		panic(err)
	}
	ciphertext := make([]byte, len(padded))
	cipher.NewCBCEncrypter(block, iv).CryptBlocks(ciphertext, padded)
	envelope := append(append([]byte(nil), iv...), ciphertext...)
	mac := hmac.New(sha256.New, key[:])
	_, _ = mac.Write(envelope)
	envelope = mac.Sum(envelope)
	return base64.StdEncoding.EncodeToString(envelope)
}
