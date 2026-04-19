// Package scan provides secret detection using gitleaks, producing redacted
// copies of data with findings describing what was detected.
package scan

import (
	"fmt"
	"sort"
	"sync"

	"github.com/rs/zerolog"
	"github.com/zricethezav/gitleaks/v8/detect"
)

var configureLoggingOnce sync.Once

// Finding describes a single secret detected in a byte slice.
type Finding struct {
	Type      string // rule description (e.g. "AWS Access Key")
	StartByte int    // start offset in the original data
	EndByte   int    // end offset (exclusive) in the original data
}

// detectFindings runs gitleaks on data and returns the raw findings without
// modifying data. It is the shared core used by Scan and ScanReport.
func detectFindings(path string, data []byte) ([]Finding, error) {
	detector, err := detect.NewDetectorDefaultConfig()
	if err != nil {
		return nil, fmt.Errorf("gitleaks config: %w", err)
	}

	fragment := detect.Fragment{
		Raw:      string(data),
		FilePath: path,
	}

	gitleaksFindings := detector.Detect(fragment)
	if len(gitleaksFindings) == 0 {
		return nil, nil
	}

	var findings []Finding
	for _, f := range gitleaksFindings {
		startIdx := -1
		endIdx := -1

		secret := f.Secret
		if secret != "" {
			for i := 0; i <= len(data)-len(secret); i++ {
				if string(data[i:i+len(secret)]) == secret {
					startIdx = i
					endIdx = i + len(secret)
					break
				}
			}
		}

		if startIdx >= 0 {
			findings = append(findings, Finding{
				Type:      f.RuleID,
				StartByte: startIdx,
				EndByte:   endIdx,
			})
		}
	}

	// Return sorted ascending by start byte.
	sort.Slice(findings, func(i, j int) bool {
		return findings[i].StartByte < findings[j].StartByte
	})

	return findings, nil
}

// Scan runs the gitleaks detector on the given data (associated with path for
// context) and returns a redacted copy of the data plus all findings.
// If no secrets are found, redacted is identical to data and findings is nil.
func Scan(path string, data []byte) (redacted []byte, findings []Finding, err error) {
	configureLoggingOnce.Do(func() {
		zerolog.SetGlobalLevel(zerolog.ErrorLevel)
	})

	findings, err = detectFindings(path, data)
	if err != nil {
		return nil, nil, err
	}
	if len(findings) == 0 {
		return data, nil, nil
	}

	// Sort descending so replacements from the end don't shift earlier offsets.
	descFindings := make([]Finding, len(findings))
	copy(descFindings, findings)
	sort.Slice(descFindings, func(i, j int) bool {
		return descFindings[i].StartByte > descFindings[j].StartByte
	})

	// Build redacted copy.
	redacted = make([]byte, len(data))
	copy(redacted, data)
	for _, f := range descFindings {
		replacement := []byte(fmt.Sprintf("<REDACTED:%s>", f.Type))
		redacted = append(redacted[:f.StartByte], append(replacement, redacted[f.EndByte:]...)...)
	}

	return redacted, findings, nil
}

// ScanReport runs the gitleaks detector on data without modifying it.
// It returns findings describing detected secrets but leaves data untouched.
// This is the correct function to use when the caller will encrypt the archive,
// making in-place redaction unnecessary and lossy.
//
// Callers MUST treat a non-nil error as a hard failure: if the scanner cannot
// initialise, the backup must not proceed (fail-closed security posture).
func ScanReport(path string, data []byte) ([]Finding, error) {
	configureLoggingOnce.Do(func() {
		zerolog.SetGlobalLevel(zerolog.ErrorLevel)
	})
	return detectFindings(path, data)
}
