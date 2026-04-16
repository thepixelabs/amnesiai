package crypto_test

import (
	"os"
	"testing"

	"github.com/thepixelabs/amensiai/internal/crypto"
)

// TestEncryptDecryptRoundTrip verifies that encrypting then decrypting returns
// the original bytes intact.
func TestEncryptDecryptRoundTrip(t *testing.T) {
	cases := []struct {
		name       string
		passphrase string
		plaintext  []byte
	}{
		{
			name:       "short ascii content",
			passphrase: "s3cr3t",
			plaintext:  []byte("hello, world"),
		},
		{
			name:       "binary content",
			passphrase: "passw0rd!",
			plaintext:  []byte{0x00, 0x01, 0xFE, 0xFF, 0x42},
		},
		{
			name:       "empty content",
			passphrase: "passphrase",
			plaintext:  []byte{},
		},
		{
			name:       "unicode content",
			passphrase: "pässphräse",
			plaintext:  []byte("日本語テキスト"),
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ciphertext, err := crypto.Encrypt(tc.passphrase, tc.plaintext)
			if err != nil {
				t.Fatalf("Encrypt: unexpected error: %v", err)
			}

			got, err := crypto.Decrypt(tc.passphrase, ciphertext)
			if err != nil {
				t.Fatalf("Decrypt: unexpected error: %v", err)
			}

			if string(got) != string(tc.plaintext) {
				t.Errorf("round-trip mismatch: got %q, want %q", got, tc.plaintext)
			}
		})
	}
}

// TestDecryptWrongPassphraseReturnsError verifies that decryption with the
// wrong passphrase produces an error, not silently corrupted data.
func TestDecryptWrongPassphraseReturnsError(t *testing.T) {
	plaintext := []byte("sensitive config data")
	ciphertext, err := crypto.Encrypt("correct-passphrase", plaintext)
	if err != nil {
		t.Fatalf("Encrypt: %v", err)
	}

	_, err = crypto.Decrypt("wrong-passphrase", ciphertext)
	if err == nil {
		t.Fatal("expected error decrypting with wrong passphrase, got nil")
	}
}

// TestEmptyPassphraseSkipsEncryption verifies that an empty passphrase causes
// Encrypt to return the plaintext unchanged, and Decrypt to return ciphertext
// unchanged — i.e., encryption is a no-op.
func TestEmptyPassphraseSkipsEncryption(t *testing.T) {
	original := []byte("plaintext config")

	encrypted, err := crypto.Encrypt("", original)
	if err != nil {
		t.Fatalf("Encrypt with empty passphrase: %v", err)
	}
	if string(encrypted) != string(original) {
		t.Errorf("Encrypt with empty passphrase should return plaintext unchanged; got %q", encrypted)
	}

	decrypted, err := crypto.Decrypt("", encrypted)
	if err != nil {
		t.Fatalf("Decrypt with empty passphrase: %v", err)
	}
	if string(decrypted) != string(original) {
		t.Errorf("Decrypt with empty passphrase should return input unchanged; got %q", decrypted)
	}
}

// TestPassphraseFromEnvOrFlag_EnvTakesPriority verifies that when both env var
// and flag are set, the env var wins.
func TestPassphraseFromEnvOrFlag_EnvTakesPriority(t *testing.T) {
	t.Setenv("AMENSIAI_PASSPHRASE", "from-env")

	got := crypto.PassphraseFromEnvOrFlag("from-flag")
	if got != "from-env" {
		t.Errorf("expected env value %q, got %q", "from-env", got)
	}
}

// TestPassphraseFromEnvOrFlag_FlagUsedWhenEnvEmpty verifies that the flag
// value is returned when the env var is not set.
func TestPassphraseFromEnvOrFlag_FlagUsedWhenEnvEmpty(t *testing.T) {
	// Ensure the env var is absent for this test.
	if err := os.Unsetenv("AMENSIAI_PASSPHRASE"); err != nil {
		t.Fatalf("unsetenv: %v", err)
	}

	got := crypto.PassphraseFromEnvOrFlag("from-flag")
	if got != "from-flag" {
		t.Errorf("expected flag value %q, got %q", "from-flag", got)
	}
}

// TestPassphraseFromEnvOrFlag_BothEmptyReturnsEmpty verifies that when neither
// env var nor flag are set, an empty string is returned (encryption is optional).
func TestPassphraseFromEnvOrFlag_BothEmptyReturnsEmpty(t *testing.T) {
	if err := os.Unsetenv("AMENSIAI_PASSPHRASE"); err != nil {
		t.Fatalf("unsetenv: %v", err)
	}

	got := crypto.PassphraseFromEnvOrFlag("")
	if got != "" {
		t.Errorf("expected empty string, got %q", got)
	}
}
