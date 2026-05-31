package cmd

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/spf13/cobra"

	"github.com/thepixelabs/amnesiai/internal/config"
	"github.com/thepixelabs/amnesiai/internal/storage"
)

// ----------------------------------------------------------------------------
// Task 3: cmd-layer wiring tests for the --files flag on restoreCmd.
//
// Design note: restoreCmd is a package-level *cobra.Command whose flags were
// registered once by init().  Mutating it via ParseFlags leaves persistent
// state that leaks across tests.  The flag-parsing shape tests therefore use
// a fresh throwaway command whose flag set mirrors the one in restore.go,
// isolating each test from global state.  Only the end-to-end runRestore
// tests use the real restoreCmd (they restore flags via ResetFlags+re-register
// inside the test body and clean up with t.Cleanup).
// ----------------------------------------------------------------------------

// newFilesCmd returns a fresh cobra.Command that carries only the --files flag
// (the flag under test) so flag-parsing shape tests stay isolated.
func newFilesCmd() *cobra.Command {
	c := &cobra.Command{Use: "test"}
	c.Flags().StringSlice("files", nil, "subset of archive paths to restore")
	return c
}

// TestRestoreCmd_FilesFlag_ParsesCommaSeparated verifies that the --files cobra
// flag is registered as a StringSlice and correctly splits comma-separated
// archive paths into a []string.  This is a pure flag-wiring test; no storage
// or provider setup is needed.
func TestRestoreCmd_FilesFlag_ParsesCommaSeparated(t *testing.T) {
	c := newFilesCmd()
	if err := c.ParseFlags([]string{
		"--files", "claude/agents/foo.md,claude/CLAUDE.md",
	}); err != nil {
		t.Fatalf("ParseFlags: %v", err)
	}

	got, err := c.Flags().GetStringSlice("files")
	if err != nil {
		t.Fatalf("GetStringSlice(files): %v", err)
	}
	want := []string{"claude/agents/foo.md", "claude/CLAUDE.md"}
	if len(got) != len(want) {
		t.Fatalf("--files slice len = %d, want %d; got %v", len(got), len(want), got)
	}
	for i, w := range want {
		if got[i] != w {
			t.Errorf("--files[%d] = %q, want %q", i, got[i], w)
		}
	}
}

// TestRestoreCmd_FilesFlag_ParsesRepeatedFlag verifies that --files can be
// supplied multiple times (cobra StringSlice accumulates repeated flags).
func TestRestoreCmd_FilesFlag_ParsesRepeatedFlag(t *testing.T) {
	c := newFilesCmd()
	if err := c.ParseFlags([]string{
		"--files", "claude/agents/foo.md",
		"--files", "gemini/config.json",
	}); err != nil {
		t.Fatalf("ParseFlags: %v", err)
	}

	got, err := c.Flags().GetStringSlice("files")
	if err != nil {
		t.Fatalf("GetStringSlice(files): %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("repeated --files: want 2 entries, got %d: %v", len(got), got)
	}
	found := map[string]bool{
		"claude/agents/foo.md": false,
		"gemini/config.json":   false,
	}
	for _, p := range got {
		found[p] = true
	}
	for want, ok := range found {
		if !ok {
			t.Errorf("repeated --files: expected %q in result; got %v", want, got)
		}
	}
}

// TestRestoreCmd_FilesFlag_EmptyMeansNoFilter verifies that omitting --files
// results in an empty slice (i.e. the "restore all files" default).
func TestRestoreCmd_FilesFlag_EmptyMeansNoFilter(t *testing.T) {
	c := newFilesCmd()
	// No ParseFlags call; the flag must default to nil/empty.
	got, err := c.Flags().GetStringSlice("files")
	if err != nil {
		t.Fatalf("GetStringSlice(files) without setting flag: %v", err)
	}
	if len(got) != 0 {
		t.Errorf("--files default: want empty slice, got %v", got)
	}
}

// TestRestoreCmd_AllUnknownFiles_ReturnsError verifies the end-to-end error
// path: when every path supplied to --files is absent from the backup,
// runRestore surfaces the "no files matched" error rather than silently
// succeeding with zero files.
//
// Setup: craft a minimal backup archive in-memory (no provider required),
// inject it into a local store, then call runRestore directly on the real
// restoreCmd.  The test re-registers flags after ResetFlags and restores them
// in t.Cleanup.
func TestRestoreCmd_AllUnknownFiles_ReturnsError(t *testing.T) {
	backupDir := t.TempDir()
	outDir := t.TempDir()

	// Wire the package-level cfg so getStorage() returns a real local store.
	prev := cfg
	t.Cleanup(func() { cfg = prev })
	cfg = config.Config{
		StorageMode: "local",
		BackupDir:   backupDir,
	}

	store, err := storage.New("local", backupDir)
	if err != nil {
		t.Fatalf("storage.New: %v", err)
	}

	// Save a minimal backup with one known file.
	archivePayload := buildRestoreTestArchive(t, "testprovider", "known.md", []byte("content"))
	meta := storage.Metadata{
		Timestamp: time.Now().UTC(),
		Providers: []string{"testprovider"},
		Encrypted: false,
	}
	backupID, err := store.Save("test", meta, archivePayload)
	if err != nil {
		t.Fatalf("store.Save: %v", err)
	}

	// Reset flags so we can call ParseFlags cleanly on the shared restoreCmd,
	// then re-register all flags that init() originally registered.
	restoreCmd.ResetFlags()
	restoreCmd.Flags().String("id", "", "")
	restoreCmd.Flags().StringSlice("providers", nil, "")
	restoreCmd.Flags().Bool("dry-run", false, "")
	restoreCmd.Flags().String("out-dir", "", "")
	restoreCmd.Flags().Bool("force", false, "")
	restoreCmd.Flags().StringSlice("files", nil, "")

	t.Cleanup(func() {
		// Restore flags to their original registered state so other tests in
		// the cmd package are unaffected.
		restoreCmd.ResetFlags()
		restoreCmd.Flags().String("id", "", "backup ID to restore (default: latest)")
		restoreCmd.Flags().StringSlice("providers", nil, "subset of providers to restore")
		restoreCmd.Flags().Bool("dry-run", false, "show what would be restored without writing")
		restoreCmd.Flags().String("out-dir", "", "extract files into this directory instead of overwriting real destinations (mirrors the destination layout)")
		restoreCmd.Flags().Bool("force", false, "with --out-dir: allow writing into a non-empty directory (existing files are never deleted)")
		restoreCmd.Flags().StringSlice("files", nil, "subset of archive paths to restore (e.g. claude/agents/foo.md). Empty = all files in selected providers")
	})

	if err := restoreCmd.ParseFlags([]string{
		"--id", backupID,
		"--files", "testprovider/does/not/exist.md",
		"--out-dir", outDir,
	}); err != nil {
		t.Fatalf("ParseFlags: %v", err)
	}

	// runRestore calls getStorage() which reads cfg — cfg is set to our temp dir above.
	runErr := runRestore(restoreCmd, nil)
	if runErr == nil {
		t.Fatal("expected non-nil error when all --files paths are unknown; got nil")
	}
	if !strings.Contains(runErr.Error(), "no files matched") {
		t.Errorf("error %q does not mention 'no files matched'", runErr.Error())
	}
}

// ----------------------------------------------------------------------------
// Helpers
// ----------------------------------------------------------------------------

// buildRestoreTestArchive constructs a valid tar.gz backup payload containing
// one file and a manifest.json, matching the format expected by
// core.ExtractArchive.  provName is the provider name (e.g. "testprovider"),
// relPath is the file's original path relative to the provider root, and
// content is the file body.
func buildRestoreTestArchive(t *testing.T, provName, relPath string, content []byte) []byte {
	t.Helper()

	archPath := provName + "/" + relPath

	type manifestEntry struct {
		Provider string `json:"provider"`
		ArchPath string `json:"arch_path"`
		OrigPath string `json:"orig_path"`
	}
	manifest := []manifestEntry{{
		Provider: provName,
		ArchPath: archPath,
		OrigPath: relPath,
	}}
	manifestBytes, err := json.Marshal(manifest)
	if err != nil {
		t.Fatalf("buildRestoreTestArchive: marshal manifest: %v", err)
	}

	var buf bytes.Buffer
	gw := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gw)

	writeEntry := func(name string, data []byte) {
		t.Helper()
		hdr := &tar.Header{
			Name:    name,
			Size:    int64(len(data)),
			Mode:    0600,
			ModTime: time.Now().UTC(),
		}
		if err := tw.WriteHeader(hdr); err != nil {
			t.Fatalf("buildRestoreTestArchive WriteHeader(%s): %v", name, err)
		}
		if _, err := tw.Write(data); err != nil {
			t.Fatalf("buildRestoreTestArchive Write(%s): %v", name, err)
		}
	}

	writeEntry(archPath, content)
	writeEntry("manifest.json", manifestBytes)

	if err := tw.Close(); err != nil {
		t.Fatalf("buildRestoreTestArchive tw.Close: %v", err)
	}
	if err := gw.Close(); err != nil {
		t.Fatalf("buildRestoreTestArchive gw.Close: %v", err)
	}
	return buf.Bytes()
}
