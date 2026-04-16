// Package core orchestrates backup, restore, and diff operations across
// all providers, storage backends, encryption, and secret scanning.
package core

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"

	"github.com/thepixelabs/amensiai/internal/crypto"
	"github.com/thepixelabs/amensiai/internal/provider"
	"github.com/thepixelabs/amensiai/internal/scan"
	"github.com/thepixelabs/amensiai/internal/storage"
)

// BackupOptions controls the backup operation.
type BackupOptions struct {
	Providers  []string          // provider names to back up
	Passphrase string            // encryption passphrase (empty = no encryption)
	Labels     map[string]string // user-defined labels
	Message    string            // optional commit message override
}

// BackupResult holds the outcome of a backup operation.
type BackupResult struct {
	ID        string
	Timestamp time.Time
	Providers []string
	Findings  map[string][]scan.Finding // per-provider secret findings
}

// fileEntry holds a single file's provider, relative path, and content for
// archiving.
type fileEntry struct {
	providerName string
	path         string
	data         []byte
}

// Backup performs a full backup: discover files from each provider, scan for
// secrets, archive into a tar.gz, optionally encrypt, and save to storage.
func Backup(store storage.Storage, opts BackupOptions) (*BackupResult, error) {
	providers, err := provider.GetMultiple(opts.Providers)
	if err != nil {
		return nil, fmt.Errorf("get providers: %w", err)
	}

	allFindings := make(map[string][]scan.Finding)

	// Collect and scan data from all providers.
	// Track the names of providers that were actually resolved (may differ from
	// opts.Providers when opts.Providers is empty and all registered providers
	// are used).
	var files []fileEntry
	actualProviderNames := make([]string, 0, len(providers))

	for _, p := range providers {
		actualProviderNames = append(actualProviderNames, p.Name())
		snapshot, err := p.Read()
		if err != nil {
			return nil, fmt.Errorf("read provider %s: %w", p.Name(), err)
		}

		for path, data := range snapshot {
			// Scan for secrets before archiving.
			redacted, findings, scanErr := scan.Scan(path, data)
			if scanErr != nil {
				// Warn loudly: the user must know secrets may not be redacted.
				fmt.Fprintf(os.Stderr, "WARNING: secret scan failed for %s/%s: %v — file included unscanned\n",
					p.Name(), path, scanErr)
				redacted = data
			}
			if len(findings) > 0 {
				allFindings[p.Name()] = append(allFindings[p.Name()], findings...)
			}

			files = append(files, fileEntry{
				providerName: p.Name(),
				path:         path,
				data:         redacted,
			})
		}
	}

	// Create tar.gz archive.
	payload, err := createArchive(files)
	if err != nil {
		return nil, fmt.Errorf("create archive: %w", err)
	}

	// Encrypt if passphrase is provided.
	payload, err = crypto.Encrypt(opts.Passphrase, payload)
	if err != nil {
		return nil, fmt.Errorf("encrypt: %w", err)
	}

	// Build metadata using the names of providers that were actually resolved,
	// not opts.Providers (which may be empty when all providers are requested).
	now := time.Now().UTC()
	meta := storage.Metadata{
		ID:        now.Format("20060102T150405Z"),
		Timestamp: now,
		Providers: actualProviderNames,
		Labels:    opts.Labels,
	}

	// Save to storage.
	id, err := store.Save("backup", meta, payload)
	if err != nil {
		return nil, fmt.Errorf("save backup: %w", err)
	}

	return &BackupResult{
		ID:        id,
		Timestamp: now,
		Providers: actualProviderNames,
		Findings:  allFindings,
	}, nil
}

// createArchive packs file entries into a tar.gz byte slice.
// Files are stored as <providerName>/<relative-path> inside the archive.
func createArchive(files []fileEntry) ([]byte, error) {
	var buf bytes.Buffer
	gw := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gw)

	for _, f := range files {
		// Use provider name as top-level directory, then the full relative path.
		// Using the full path (not just the basename) prevents collisions when
		// two files share a basename in different subdirectories.
		name := filepath.Join(f.providerName, filepath.ToSlash(f.path))

		header := &tar.Header{
			Name:    name,
			Size:    int64(len(f.data)),
			Mode:    0600,
			ModTime: time.Now().UTC(),
		}
		if err := tw.WriteHeader(header); err != nil {
			return nil, fmt.Errorf("write tar header for %s: %w", name, err)
		}
		if _, err := tw.Write(f.data); err != nil {
			return nil, fmt.Errorf("write tar data for %s: %w", name, err)
		}
	}

	// Write manifest with original paths.
	type manifestEntry struct {
		Provider string `json:"provider"`
		ArchPath string `json:"arch_path"`
		OrigPath string `json:"orig_path"`
	}
	var manifest []manifestEntry
	for _, f := range files {
		manifest = append(manifest, manifestEntry{
			Provider: f.providerName,
			ArchPath: filepath.Join(f.providerName, filepath.ToSlash(f.path)),
			OrigPath: f.path,
		})
	}
	manifestBytes, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("marshal manifest: %w", err)
	}
	mHeader := &tar.Header{
		Name:    "manifest.json",
		Size:    int64(len(manifestBytes)),
		Mode:    0600,
		ModTime: time.Now().UTC(),
	}
	if err := tw.WriteHeader(mHeader); err != nil {
		return nil, fmt.Errorf("write manifest header: %w", err)
	}
	if _, err := tw.Write(manifestBytes); err != nil {
		return nil, fmt.Errorf("write manifest data: %w", err)
	}

	if err := tw.Close(); err != nil {
		return nil, fmt.Errorf("close tar: %w", err)
	}
	if err := gw.Close(); err != nil {
		return nil, fmt.Errorf("close gzip: %w", err)
	}

	return buf.Bytes(), nil
}

// ExtractArchive unpacks a tar.gz archive and returns a map of archive path to content,
// plus the manifest entries for path mapping.
func ExtractArchive(payload []byte) (map[string][]byte, []ManifestEntry, error) {
	gr, err := gzip.NewReader(bytes.NewReader(payload))
	if err != nil {
		return nil, nil, fmt.Errorf("open gzip: %w", err)
	}
	defer gr.Close()

	tr := tar.NewReader(gr)
	files := make(map[string][]byte)

	for {
		header, err := tr.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, nil, fmt.Errorf("read tar: %w", err)
		}

		data, err := io.ReadAll(tr)
		if err != nil {
			return nil, nil, fmt.Errorf("read tar entry %s: %w", header.Name, err)
		}
		files[header.Name] = data
	}

	// Parse manifest if present.
	var manifest []ManifestEntry
	if manifestData, ok := files["manifest.json"]; ok {
		if err := json.Unmarshal(manifestData, &manifest); err != nil {
			return nil, nil, fmt.Errorf("parse manifest: %w", err)
		}
		delete(files, "manifest.json")
	}

	return files, manifest, nil
}

// ManifestEntry maps archive paths back to original filesystem paths.
type ManifestEntry struct {
	Provider string `json:"provider"`
	ArchPath string `json:"arch_path"`
	OrigPath string `json:"orig_path"`
}
