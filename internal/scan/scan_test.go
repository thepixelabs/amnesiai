package scan_test

import (
	"bytes"
	"strings"
	"testing"

	"github.com/thepixelabs/amnesiai/internal/scan"
)

// TestScan_RedactsAWSAccessToken verifies that a string containing an AWS
// access key pattern is redacted and the finding is reported.
func TestScan_RedactsAWSAccessToken(t *testing.T) {
	// AKIA1234567890ABCDEF matches the aws-access-token gitleaks rule.
	// It is not in gitleaks' allowlist (only "EXAMPLE" variants are).
	input := []byte("ACCESS_KEY_ID=AKIA1234567890ABCDEF")

	redacted, findings, err := scan.Scan("settings.json", input)
	if err != nil {
		t.Fatalf("Scan: unexpected error: %v", err)
	}

	if len(findings) == 0 {
		t.Fatal("expected at least one finding for AWS access key, got none")
	}

	// Confirm the finding carries the correct rule ID.
	found := false
	for _, f := range findings {
		if f.Type == "aws-access-token" {
			found = true
		}
	}
	if !found {
		t.Errorf("expected finding with Type %q; got findings: %+v", "aws-access-token", findings)
	}

	// Confirm the raw key is gone from the redacted output.
	if bytes.Contains(redacted, []byte("AKIA1234567890ABCDEF")) {
		t.Errorf("redacted output still contains the raw secret: %q", redacted)
	}

	// Confirm the redaction placeholder is present.
	want := "<REDACTED:aws-access-token>"
	if !strings.Contains(string(redacted), want) {
		t.Errorf("redacted output does not contain %q; got: %q", want, redacted)
	}
}

// TestScan_CleanContentPassesThroughUnchanged verifies that content with no
// secrets is returned byte-for-byte identical with zero findings.
func TestScan_CleanContentPassesThroughUnchanged(t *testing.T) {
	input := []byte(`{"theme": "dark", "fontSize": 14, "autoSave": true}`)

	redacted, findings, err := scan.Scan("settings.json", input)
	if err != nil {
		t.Fatalf("Scan: unexpected error: %v", err)
	}

	if len(findings) != 0 {
		t.Errorf("expected no findings for clean content, got: %+v", findings)
	}

	if !bytes.Equal(redacted, input) {
		t.Errorf("clean content was modified:\n  got:  %q\n  want: %q", redacted, input)
	}
}

// TestScan_IdempotentOnAlreadyRedactedContent verifies that scanning content
// that already contains a redaction placeholder does not produce additional
// redactions or double-wrap the placeholder.
func TestScan_IdempotentOnAlreadyRedactedContent(t *testing.T) {
	// Start with a raw finding, redact it, then scan the redacted output again.
	original := []byte("ACCESS_KEY_ID=AKIA1234567890ABCDEF")

	firstPass, _, err := scan.Scan("settings.json", original)
	if err != nil {
		t.Fatalf("first Scan: %v", err)
	}

	secondPass, findings2, err := scan.Scan("settings.json", firstPass)
	if err != nil {
		t.Fatalf("second Scan: %v", err)
	}

	// The second scan should find nothing new (the placeholder itself is not a secret).
	if len(findings2) != 0 {
		t.Errorf("second scan found unexpected findings in already-redacted content: %+v", findings2)
	}

	// The output should be unchanged from the first pass.
	if !bytes.Equal(secondPass, firstPass) {
		t.Errorf("second scan changed already-redacted content:\n  got:  %q\n  want: %q", secondPass, firstPass)
	}
}
