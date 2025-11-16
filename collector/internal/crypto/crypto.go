package crypto

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"os"
	"sync"
)

var (
	ErrInvalidKeyLength  = errors.New("encryption key must be 32 bytes")
	ErrInvalidCiphertext = errors.New("invalid ciphertext format")
)

var (
	cachedKey []byte
	keyOnce   sync.Once
	keyErr    error
)

// getEncryptionKey retrieves the encryption key from environment variable.
// The key must be exactly 32 bytes for AES-256.
// Cached after first load for performance.
func getEncryptionKey() ([]byte, error) {
	keyOnce.Do(func() {
		key := os.Getenv("MEOWS_ENCRYPTION_KEY")
		if key == "" {
			keyErr = errors.New("MEOWS_ENCRYPTION_KEY environment variable not set")
			return
		}

		keyBytes := []byte(key)
		if len(keyBytes) != 32 {
			keyErr = fmt.Errorf("%w: got %d bytes, need 32", ErrInvalidKeyLength, len(keyBytes))
			return
		}

		cachedKey = keyBytes
	})

	return cachedKey, keyErr
}

// Encrypt encrypts plaintext using AES-GCM and returns base64-encoded ciphertext.
// Note: Always encrypts, even empty strings. Use NULL in DB to represent "not set".
func Encrypt(plaintext string) (string, error) {

	key, err := getEncryptionKey()
	if err != nil {
		return "", err
	}

	block, err := aes.NewCipher(key)
	if err != nil {
		return "", fmt.Errorf("failed to create cipher: %w", err)
	}

	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", fmt.Errorf("failed to create GCM: %w", err)
	}

	nonce := make([]byte, gcm.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return "", fmt.Errorf("failed to generate nonce: %w", err)
	}

	ciphertext := gcm.Seal(nonce, nonce, []byte(plaintext), nil)
	return base64.StdEncoding.EncodeToString(ciphertext), nil
}

// Decrypt decrypts base64-encoded ciphertext using AES-GCM.
// Note: Empty ciphertext is invalid and returns error. Use NULL in DB for "not set".
func Decrypt(ciphertext string) (string, error) {
	if ciphertext == "" {
		return "", errors.New("cannot decrypt empty string, expected NULL in DB")
	}

	key, err := getEncryptionKey()
	if err != nil {
		return "", err
	}

	data, err := base64.StdEncoding.DecodeString(ciphertext)
	if err != nil {
		return "", fmt.Errorf("failed to decode base64: %w", err)
	}

	block, err := aes.NewCipher(key)
	if err != nil {
		return "", fmt.Errorf("failed to create cipher: %w", err)
	}

	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", fmt.Errorf("failed to create GCM: %w", err)
	}

	nonceSize := gcm.NonceSize()
	if len(data) < nonceSize {
		return "", ErrInvalidCiphertext
	}

	nonce, ciphertextBytes := data[:nonceSize], data[nonceSize:]
	plaintext, err := gcm.Open(nil, nonce, ciphertextBytes, nil)
	if err != nil {
		return "", fmt.Errorf("failed to decrypt: %w", err)
	}

	return string(plaintext), nil
}
