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

// Scan runs the gitleaks detector on the given data (associated with path for
// context) and returns a redacted copy of the data plus all findings.
// If no secrets are found, redacted is identical to data and findings is nil.
func Scan(path string, data []byte) (redacted []byte, findings []Finding, err error) {
	configureLoggingOnce.Do(func() {
		zerolog.SetGlobalLevel(zerolog.ErrorLevel)
	})

	detector, err := detect.NewDetectorDefaultConfig()
	if err != nil {
		// If gitleaks config fails, return data unmodified rather than blocking the user.
		return data, nil, fmt.Errorf("gitleaks config: %w", err)
	}

	fragment := detect.Fragment{
		Raw:      string(data),
		FilePath: path,
	}

	gitleaksFindings := detector.Detect(fragment)
	if len(gitleaksFindings) == 0 {
		return data, nil, nil
	}

	// Convert gitleaks findings to our Finding type.
	for _, f := range gitleaksFindings {
		startIdx := -1
		endIdx := -1

		// Find the byte offsets of the secret in the data.
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

	// Sort findings by start byte descending so we can replace from the end
	// without invalidating earlier offsets.
	sort.Slice(findings, func(i, j int) bool {
		return findings[i].StartByte > findings[j].StartByte
	})

	// Build redacted copy.
	redacted = make([]byte, len(data))
	copy(redacted, data)

	for _, f := range findings {
		replacement := []byte(fmt.Sprintf("<REDACTED:%s>", f.Type))
		redacted = append(redacted[:f.StartByte], append(replacement, redacted[f.EndByte:]...)...)
	}

	// Re-sort findings by start byte ascending for the caller.
	sort.Slice(findings, func(i, j int) bool {
		return findings[i].StartByte < findings[j].StartByte
	})

	return redacted, findings, nil
}
