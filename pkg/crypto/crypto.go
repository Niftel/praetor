package crypto

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"io"
)

// Encrypt encrypts plainText using the provided key with AES-GCM.
// The nonce is prepended to the ciphertext and the result is base64 encoded.
func Encrypt(plainText, key string) (string, error) {
	if len(key) != 32 {
		return "", fmt.Errorf("key must be 32 bytes long")
	}

	block, err := aes.NewCipher([]byte(key))
	if err != nil {
		return "", err
	}

	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", err
	}

	nonce := make([]byte, gcm.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return "", err
	}

	cipherText := gcm.Seal(nonce, nonce, []byte(plainText), nil)
	return base64.StdEncoding.EncodeToString(cipherText), nil
}

// Decrypt decrypts the base64 encoded ciphertext using the provided key.
func Decrypt(cipherText, key string) (string, error) {
	if len(key) != 32 {
		return "", fmt.Errorf("key must be 32 bytes long")
	}

	data, err := base64.StdEncoding.DecodeString(cipherText)
	if err != nil {
		return "", err
	}

	block, err := aes.NewCipher([]byte(key))
	if err != nil {
		return "", err
	}

	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", err
	}

	nonceSize := gcm.NonceSize()
	if len(data) < nonceSize {
		return "", fmt.Errorf("ciphertext too short")
	}

	nonce, text := data[:nonceSize], data[nonceSize:]
	plainText, err := gcm.Open(nil, nonce, text, nil)
	if err != nil {
		return "", err
	}

	return string(plainText), nil
}
