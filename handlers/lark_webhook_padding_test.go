package handlers

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/sha256"
	"encoding/base64"
	"testing"
)

func encryptForLarkTest(t *testing.T, encryptKey string, iv, plaintext []byte) string {
	t.Helper()
	key := sha256.Sum256([]byte(encryptKey))
	block, err := aes.NewCipher(key[:])
	if err != nil {
		t.Fatalf("aes.NewCipher: %v", err)
	}
	ciphertext := make([]byte, len(plaintext))
	cipher.NewCBCEncrypter(block, iv).CryptBlocks(ciphertext, plaintext)
	return base64.StdEncoding.EncodeToString(append(append([]byte{}, iv...), ciphertext...))
}

// BUG-15: decryptLarkPayload checks the trailing PKCS7 padding byte, but when
// it's out of range (<=0 or > block size) the function silently returns the
// whole decrypted buffer with a nil error instead of failing. That lets a
// corrupted/tampered payload be parsed as if it were valid. See
// BUG_REPORT.md BUG-15.
func TestDecryptLarkPayload_RejectsInvalidPadding(t *testing.T) {
	const encryptKey = "test-encrypt-key"
	iv := []byte("0123456789abcdef")

	// A full 16-byte block whose last byte (0x00) is not a valid PKCS7 pad value.
	plaintext := []byte("payload-data!!!\x00")
	if len(plaintext) != aes.BlockSize {
		t.Fatalf("test setup: plaintext must be exactly one AES block, got %d bytes", len(plaintext))
	}

	cipherB64 := encryptForLarkTest(t, encryptKey, iv, plaintext)

	_, err := decryptLarkPayload(encryptKey, cipherB64)
	if err == nil {
		t.Fatalf("decryptLarkPayload accepted a payload with an invalid PKCS7 padding byte instead " +
			"of returning an error (BUG-15)")
	}
}

func TestDecryptLarkPayload_AcceptsValidPadding(t *testing.T) {
	const encryptKey = "test-encrypt-key"
	iv := []byte("0123456789abcdef")

	// "hello" + 11 bytes of 0x0B padding (valid PKCS7 for a 16-byte block).
	message := "hello"
	pad := aes.BlockSize - len(message)
	plaintext := append([]byte(message), make([]byte, pad)...)
	for i := len(message); i < len(plaintext); i++ {
		plaintext[i] = byte(pad)
	}

	cipherB64 := encryptForLarkTest(t, encryptKey, iv, plaintext)

	got, err := decryptLarkPayload(encryptKey, cipherB64)
	if err != nil {
		t.Fatalf("decryptLarkPayload with valid padding returned error: %v", err)
	}
	if string(got) != message {
		t.Fatalf("decryptLarkPayload() = %q, want %q", got, message)
	}
}
