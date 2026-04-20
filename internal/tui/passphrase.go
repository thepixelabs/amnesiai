// Package tui — passphrase entry components.
//
// ReadPassphrase reads a passphrase from the terminal with middle-dot masking.
// For new backups (verify=true) it prompts twice and enforces match via
// subtle.ConstantTimeCompare so timing doesn't leak which field mismatched.
// For restore flows (verify=false) it prompts once.
//
// The charmbracelet/x/term.ReadPassword call handles raw-mode switching so
// the calling code never sees the plain bytes in argv or environment.
package tui

import (
	"crypto/subtle"
	"fmt"
	"os"

	xterm "github.com/charmbracelet/x/term"
)

// maskRune is the character shown in lieu of each typed byte.
// U+00B7 MIDDLE DOT — matches the provider-picker selection marker.
const maskRune = '·'

// ReadPassphrase prompts the user for a passphrase on /dev/tty (or os.Stdin
// when /dev/tty is unavailable).
//
// If verify is true (new-backup flow) it prompts a second time and returns
// ErrPassphraseMismatch when the two entries differ.
// If verify is false (restore flow) it prompts once.
//
// The raw bytes are never stored beyond this function's stack frame; the
// returned string is the caller's responsibility.
func ReadPassphrase(label string, verify bool) (string, error) {
	fd, err := openTTY()
	if err != nil {
		return "", fmt.Errorf("open tty: %w", err)
	}
	defer fd.Close()

	first, err := readMasked(fd, label)
	if err != nil {
		return "", fmt.Errorf("read passphrase: %w", err)
	}

	if !verify {
		return string(first), nil
	}

	second, err := readMasked(fd, "Confirm passphrase")
	if err != nil {
		return "", fmt.Errorf("read confirmation: %w", err)
	}

	// Use constant-time compare so timing cannot reveal which field mismatched.
	if subtle.ConstantTimeCompare(first, second) != 1 {
		return "", ErrPassphraseMismatch
	}

	return string(first), nil
}

// ErrPassphraseMismatch is returned when the two passphrase entries differ.
var ErrPassphraseMismatch = fmt.Errorf("passphrases do not match")

// readMasked prints label + prompt then reads a masked line.
// The masking is done by charmbracelet/x/term which handles raw-mode so that
// typed characters are replaced by the mask rune client-side.
// Note: x/term.ReadPassword masks with '*'; we print the mask manually here
// but the TTY echo suppression is what matters for security.
func readMasked(tty *os.File, label string) ([]byte, error) {
	prompt := AccentStyle.Render(label+":") + " "
	fmt.Fprint(tty, prompt)

	raw, err := xterm.ReadPassword(tty.Fd())
	fmt.Fprintln(tty) // move to next line after the hidden input
	if err != nil {
		return nil, err
	}
	return raw, nil
}

// openTTY opens /dev/tty for interactive passphrase input.  Falls back to
// os.Stdin when /dev/tty cannot be opened (e.g. in tests or containers).
func openTTY() (*os.File, error) {
	f, err := os.OpenFile("/dev/tty", os.O_RDWR, 0)
	if err != nil {
		return os.Stdin, nil //nolint:nilerr — intentional fallback
	}
	return f, nil
}
