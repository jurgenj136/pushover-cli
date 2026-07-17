package e2ee

import (
	"bytes"
	"compress/gzip"
	"crypto/aes"
	"crypto/cipher"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"io"
	"time"
)

func encryptField(key []byte, random io.Reader, plaintext string) (string, error) {
	plaintextBytes := []byte(plaintext)
	defer clear(plaintextBytes)
	compressed, err := gzipBytes(plaintextBytes)
	if err != nil {
		return "", errEncryptionFailed
	}
	defer clear(compressed)

	iv := make([]byte, aes.BlockSize)
	if err := readFullNoStall(random, iv); err != nil {
		clear(iv)
		return "", errEncryptionFailed
	}
	defer clear(iv)

	padded := pkcs7Pad(compressed, aes.BlockSize)
	defer clear(padded)
	ciphertext := make([]byte, len(padded))
	defer clear(ciphertext)
	block, err := aes.NewCipher(key)
	if err != nil {
		return "", errEncryptionFailed
	}
	cipher.NewCBCEncrypter(block, iv).CryptBlocks(ciphertext, padded)

	envelope := make([]byte, 0, len(iv)+len(ciphertext)+sha256.Size)
	envelope = append(envelope, iv...)
	envelope = append(envelope, ciphertext...)
	// Pushover's compatibility format mandates reusing the AES-CBC key for HMAC.
	mac := hmac.New(sha256.New, key)
	_, _ = mac.Write(envelope)
	envelope = mac.Sum(envelope)
	defer clear(envelope)
	return base64.StdEncoding.EncodeToString(envelope), nil
}

func readFullNoStall(reader io.Reader, destination []byte) error {
	for len(destination) > 0 {
		count, err := reader.Read(destination)
		if count < 0 || count > len(destination) {
			return io.ErrNoProgress
		}
		if count > 0 {
			destination = destination[count:]
		}
		if err != nil {
			if len(destination) == 0 {
				return nil
			}
			return err
		}
		if count == 0 {
			return io.ErrNoProgress
		}
	}
	return nil
}

func gzipBytes(plaintext []byte) ([]byte, error) {
	var output bytes.Buffer
	writer, err := gzip.NewWriterLevel(&output, gzip.BestCompression)
	if err != nil {
		return nil, err
	}
	writer.ModTime = time.Unix(0, 0).UTC()
	writer.Name = ""
	writer.Comment = ""
	writer.OS = 255
	if _, err := writer.Write(plaintext); err != nil {
		_ = writer.Close()
		clear(output.Bytes())
		return nil, err
	}
	if err := writer.Close(); err != nil {
		clear(output.Bytes())
		return nil, err
	}
	return output.Bytes(), nil
}

func pkcs7Pad(input []byte, blockSize int) []byte {
	if blockSize < 1 || blockSize > 255 {
		panic("invalid PKCS#7 block size")
	}
	padding := blockSize - len(input)%blockSize
	output := make([]byte, len(input)+padding)
	copy(output, input)
	for index := len(input); index < len(output); index++ {
		// #nosec G115 -- validated blockSize bounds padding to the byte range.
		output[index] = byte(padding)
	}
	return output
}
