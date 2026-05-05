// Package crypto provides age-based passphrase encryption and decryption
// for amnesiai backup payloads.
package crypto

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"

	"filippo.io/age"
)

// ErrIncorrectPassphrase is returned by Decrypt when the supplied passphrase
// fails to decrypt the ciphertext.  Callers can errors.Is against this to
// surface a friendly "wrong password" message instead of age's internal text.
var ErrIncorrectPassphrase = errors.New("incorrect passphrase")

// scryptWorkFactor controls the cost of scrypt key derivation for new backups.
// Each +1 doubles brute-force cost — the legit user pays it once per encrypt;
// an attacker pays it on every guess against the file.  age's default is 18
// (~1s/attempt); 20 = ~4s/attempt — an honest brute-force barrier without
// noticeably slowing the user.  The factor is written into the ciphertext
// header, so older backups (factor 18) continue to decrypt without any flag.
const scryptWorkFactor = 20

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
	recipient.SetWorkFactor(scryptWorkFactor)

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
		// age returns "no identity matched any of the recipients" when the scrypt
		// passphrase is wrong.  amnesiai only uses passphrase identities, so this
		// is unambiguous — translate it to a clear ErrIncorrectPassphrase.
		if strings.Contains(err.Error(), "no identity matched") {
			return nil, ErrIncorrectPassphrase
		}
		return nil, fmt.Errorf("create age reader: %w", err)
	}

	plaintext, err := io.ReadAll(r)
	if err != nil {
		return nil, fmt.Errorf("read decrypted data: %w", err)
	}

	return plaintext, nil
}

// PassphraseFromEnvOrFlag returns the passphrase to use for encryption.
// Priority: AMNESIAI_PASSPHRASE env var > flagVal argument.
// Returns empty string if neither is set (encryption is optional).
func PassphraseFromEnvOrFlag(flagVal string) string {
	if envVal := os.Getenv("AMNESIAI_PASSPHRASE"); envVal != "" {
		return envVal
	}
	return flagVal
}
