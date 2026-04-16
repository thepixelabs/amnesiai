// Package crypto provides age-based passphrase encryption and decryption
// for amensiai backup payloads.
package crypto

import (
	"bytes"
	"fmt"
	"io"
	"os"

	"filippo.io/age"
)

// Encrypt encrypts plaintext using an age passphrase recipient.
// If passphrase is empty, returns plaintext unchanged (encryption skipped).
func Encrypt(passphrase string, plaintext []byte) ([]byte, error) {
	if passphrase == "" {
		return plaintext, nil
	}

	recipient, err := age.NewScryptRecipient(passphrase)
	if err != nil {
		return nil, fmt.Errorf("create scrypt recipient: %w", err)
	}

	var buf bytes.Buffer
	w, err := age.Encrypt(&buf, recipient)
	if err != nil {
		return nil, fmt.Errorf("create age writer: %w", err)
	}
	if _, err := w.Write(plaintext); err != nil {
		return nil, fmt.Errorf("write encrypted data: %w", err)
	}
	if err := w.Close(); err != nil {
		return nil, fmt.Errorf("close age writer: %w", err)
	}

	return buf.Bytes(), nil
}

// Decrypt decrypts ciphertext using an age passphrase identity.
// If passphrase is empty, returns ciphertext unchanged (assumes unencrypted).
func Decrypt(passphrase string, ciphertext []byte) ([]byte, error) {
	if passphrase == "" {
		return ciphertext, nil
	}

	identity, err := age.NewScryptIdentity(passphrase)
	if err != nil {
		return nil, fmt.Errorf("create scrypt identity: %w", err)
	}

	r, err := age.Decrypt(bytes.NewReader(ciphertext), identity)
	if err != nil {
		return nil, fmt.Errorf("create age reader: %w", err)
	}

	plaintext, err := io.ReadAll(r)
	if err != nil {
		return nil, fmt.Errorf("read decrypted data: %w", err)
	}

	return plaintext, nil
}

// PassphraseFromEnvOrFlag returns the passphrase to use for encryption.
// Priority: AMENSIAI_PASSPHRASE env var > flagVal argument.
// Returns empty string if neither is set (encryption is optional).
func PassphraseFromEnvOrFlag(flagVal string) string {
	if envVal := os.Getenv("AMENSIAI_PASSPHRASE"); envVal != "" {
		return envVal
	}
	return flagVal
}
