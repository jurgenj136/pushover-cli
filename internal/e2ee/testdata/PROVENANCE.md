# E2EE test-vector provenance

Pushover's [Message API encryption scheme](https://pushover.net/api) describes
the field mapping (`message`, `title`, `url`, and `url_title`) and its envelope:
gzip plaintext, AES-256-CBC with PKCS#7 padding using a fresh 16-byte IV,
HMAC-SHA256 over raw `IV || ciphertext` with the same 32-byte key, then padded
standard Base64. Retrieved 2026-07-16. Pushover publishes an algorithm and a
shell example, but no fixed ciphertext test vectors.

`vectors.json` is independently reproduced test material, not a user secret.
It used key `00112233445566778899aabbccddeeff00112233445566778899aabbccddeeff`,
IV `000102030405060708090a0b0c0d0e0f`, and UTF-8 plaintext `This has been
encrypted`. The following macOS commands generated it without a trailing
newline and with raw HMAC bytes:

```sh
KEY=00112233445566778899aabbccddeeff00112233445566778899aabbccddeeff
IV=000102030405060708090a0b0c0d0e0f
CT=$(printf %s 'This has been encrypted' | gzip -n -9 | \
  openssl enc -aes-256-cbc -K "$KEY" -iv "$IV" | xxd -p | tr -d '\n')
HMAC=$(printf %s "$IV$CT" | xxd -r -p | \
  openssl dgst -sha256 -mac HMAC -macopt hexkey:"$KEY" | awk '{print $NF}')
printf %s "$IV$CT$HMAC" | xxd -r -p | openssl base64 -A
```

Tool versions: macOS `gzip 479`, OpenSSL `3.6.2`; generated 2026-07-16.
`gzip -n -9` sets a platform-specific gzip OS header byte, whereas production
sets OS=255 for deterministic Go output. Therefore the vector is verified by
authenticated structural decryption, rather than by demanding identical DEFLATE
bytes from a different compressor.
