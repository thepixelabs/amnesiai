package cmd

import (
	"bytes"
	"testing"
)

// TestRootCommand_NoArgsNonTTY_PrintsHelpExitsZero verifies that running the
// root command with no arguments in a non-TTY environment prints help text and
// returns nil (exit zero).
func TestRootCommand_NoArgsNonTTY_PrintsHelpExitsZero(t *testing.T) {
	// Override the TTY detection so the root command takes the help path.
	orig := isTTYFn
	isTTYFn = func() bool { return false }
	t.Cleanup(func() { isTTYFn = orig })

	var buf bytes.Buffer
	rootCmd.SetOut(&buf)
	rootCmd.SetArgs([]string{})
	t.Cleanup(func() {
		rootCmd.SetArgs(nil)
		rootCmd.SetOut(nil)
		rootCmd.SetErr(nil)
	})

	err := rootCmd.Execute()
	if err != nil {
		t.Fatalf("Execute() returned error: %v", err)
	}

	out := buf.String()
	if out == "" {
		t.Fatal("Execute() produced no output; expected help text")
	}
	if !containsAny(out, "amnesiai", "backup", "restore") {
		t.Errorf("help output does not mention amnesiai or subcommands; got:\n%s", out)
	}
}

// TestRootCommand_UnknownSubcommand_ReturnsError verifies that an unrecognised
// subcommand causes Execute to return a non-nil error.
func TestRootCommand_UnknownSubcommand_ReturnsError(t *testing.T) {
	orig := isTTYFn
	isTTYFn = func() bool { return false }
	t.Cleanup(func() { isTTYFn = orig })

	var buf bytes.Buffer
	rootCmd.SetOut(&buf)
	rootCmd.SetErr(&buf)
	rootCmd.SetArgs([]string{"unknown-command-xyz"})
	t.Cleanup(func() {
		rootCmd.SetArgs(nil)
		rootCmd.SetOut(nil)
		rootCmd.SetErr(nil)
	})

	err := rootCmd.Execute()
	if err == nil {
		t.Fatal("Execute() with unknown subcommand: expected non-nil error, got nil")
	}
}

// TestRootCommand_HelpFlag_ExitsZero verifies that --help prints help and
// returns nil (exit zero).
func TestRootCommand_HelpFlag_ExitsZero(t *testing.T) {
	orig := isTTYFn
	isTTYFn = func() bool { return false }
	t.Cleanup(func() { isTTYFn = orig })

	var buf bytes.Buffer
	rootCmd.SetOut(&buf)
	rootCmd.SetErr(&buf)
	rootCmd.SetArgs([]string{"--help"})
	t.Cleanup(func() {
		rootCmd.SetArgs(nil)
		rootCmd.SetOut(nil)
		rootCmd.SetErr(nil)
	})

	err := rootCmd.Execute()
	if err != nil {
		t.Fatalf("Execute() --help returned error: %v", err)
	}

	out := buf.String()
	if !containsAny(out, "amnesiai", "backup") {
		t.Errorf("--help output does not mention amnesiai or backup; got:\n%s", out)
	}
}

// containsAny returns true if s contains at least one of the given substrings.
func containsAny(s string, subs ...string) bool {
	for _, sub := range subs {
		if len(s) >= len(sub) {
			for i := 0; i <= len(s)-len(sub); i++ {
				if s[i:i+len(sub)] == sub {
					return true
				}
			}
		}
	}
	return false
}
