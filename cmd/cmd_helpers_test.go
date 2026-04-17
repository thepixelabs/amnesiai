package cmd

import (
	"bufio"
	"strings"
	"testing"

	"github.com/thepixelabs/amnesiai/internal/core"
	"github.com/thepixelabs/amnesiai/internal/storage"
)

// ─── resolveProviders ─────────────────────────────────────────────────────────

func TestResolveProviders(t *testing.T) {
	tests := []struct {
		name      string
		input     string
		defaults  []string
		available []string
		want      []string
		wantErr   bool
	}{
		{
			name:      "EmptyInputReturnsDefaults",
			input:     "",
			defaults:  []string{"claude"},
			available: []string{"claude", "gemini"},
			want:      []string{"claude"},
			wantErr:   false,
		},
		{
			name:      "AllKeywordReturnsAll",
			input:     "all",
			defaults:  []string{"claude"},
			available: []string{"claude", "gemini"},
			want:      []string{"claude", "gemini"},
			wantErr:   false,
		},
		{
			name:      "NumericIndexValid",
			input:     "2",
			defaults:  []string{},
			available: []string{"claude", "gemini"},
			want:      []string{"gemini"},
			wantErr:   false,
		},
		{
			name:      "NamedProvider",
			input:     "gemini",
			defaults:  []string{},
			available: []string{"claude", "gemini"},
			want:      []string{"gemini"},
			wantErr:   false,
		},
		{
			name:      "MultipleNamesCSV",
			input:     "claude,gemini",
			defaults:  []string{},
			available: []string{"claude", "gemini"},
			want:      []string{"claude", "gemini"},
			wantErr:   false,
		},
		{
			name:      "Deduplicates",
			input:     "claude,claude",
			defaults:  []string{},
			available: []string{"claude", "gemini"},
			want:      []string{"claude"},
			wantErr:   false,
		},
		{
			name:      "OutOfRangeIndex",
			input:     "5",
			defaults:  []string{},
			available: []string{"claude", "gemini"},
			want:      nil,
			wantErr:   true,
		},
		{
			name:      "UnknownName",
			input:     "notreal",
			defaults:  []string{},
			available: []string{"claude", "gemini"},
			want:      nil,
			wantErr:   true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := resolveProviders(tc.input, tc.defaults, tc.available)

			if tc.wantErr {
				if err == nil {
					t.Fatalf("resolveProviders(%q, %v, %v): expected error, got nil", tc.input, tc.defaults, tc.available)
				}
				return
			}
			if err != nil {
				t.Fatalf("resolveProviders(%q, %v, %v): unexpected error: %v", tc.input, tc.defaults, tc.available, err)
			}
			if len(got) != len(tc.want) {
				t.Fatalf("resolveProviders returned %v, want %v", got, tc.want)
			}
			for i := range tc.want {
				if got[i] != tc.want[i] {
					t.Errorf("result[%d] = %q, want %q", i, got[i], tc.want[i])
				}
			}
		})
	}
}

// ─── parseLabels ─────────────────────────────────────────────────────────────

func TestParseLabels(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		want    map[string]string
		wantErr bool
	}{
		{
			name:    "EmptyReturnsEmptyMap",
			input:   "",
			want:    map[string]string{},
			wantErr: false,
		},
		{
			name:    "WhitespaceReturnsEmptyMap",
			input:   "   ",
			want:    map[string]string{},
			wantErr: false,
		},
		{
			name:    "SingleKV",
			input:   "env=prod",
			want:    map[string]string{"env": "prod"},
			wantErr: false,
		},
		{
			name:    "MultipleKV",
			input:   "env=prod,region=us",
			want:    map[string]string{"env": "prod", "region": "us"},
			wantErr: false,
		},
		{
			name:    "ValueContainsEquals",
			input:   "url=http://x=y",
			want:    map[string]string{"url": "http://x=y"},
			wantErr: false,
		},
		{
			name:    "MissingEqualsReturnsError",
			input:   "badlabel",
			want:    nil,
			wantErr: true,
		},
		{
			name:    "EmptyKeyReturnsError",
			input:   "=value",
			want:    nil,
			wantErr: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := parseLabels(tc.input)

			if tc.wantErr {
				if err == nil {
					t.Fatalf("parseLabels(%q): expected error, got nil", tc.input)
				}
				return
			}
			if err != nil {
				t.Fatalf("parseLabels(%q): unexpected error: %v", tc.input, err)
			}
			if len(got) != len(tc.want) {
				t.Fatalf("parseLabels(%q) = %v, want %v", tc.input, got, tc.want)
			}
			for k, wantV := range tc.want {
				if gotV, ok := got[k]; !ok || gotV != wantV {
					t.Errorf("parseLabels(%q)[%q] = %q, want %q", tc.input, k, gotV, wantV)
				}
			}
		})
	}
}

// ─── filterChanged ───────────────────────────────────────────────────────────

func TestFilterChanged(t *testing.T) {
	tests := []struct {
		name      string
		statuses  []string
		wantLen   int
		wantNilOk bool // true means nil result is acceptable (no changes)
	}{
		{
			name:      "EmptyInput",
			statuses:  []string{},
			wantLen:   0,
			wantNilOk: true,
		},
		{
			name:      "AllUnchangedFilteredOut",
			statuses:  []string{"unchanged", "unchanged"},
			wantLen:   0,
			wantNilOk: true,
		},
		{
			name:     "MixedFiltersCorrectly",
			statuses: []string{"added", "unchanged", "deleted"},
			wantLen:  2,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			input := make([]core.DiffEntry, len(tc.statuses))
			for i, s := range tc.statuses {
				input[i] = core.DiffEntry{Path: "file" + string(rune('A'+i)), Status: s}
			}

			got := filterChanged(input)

			if tc.wantNilOk && len(got) == 0 {
				return
			}
			if len(got) != tc.wantLen {
				t.Errorf("filterChanged: got %d entries, want %d", len(got), tc.wantLen)
			}
			for _, entry := range got {
				if entry.Status == "unchanged" {
					t.Errorf("filterChanged: returned an unchanged entry: %+v", entry)
				}
			}
		})
	}
}

// ─── statusSymbol ─────────────────────────────────────────────────────────────

func TestStatusSymbol(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{"Added", "added", "+"},
		{"Modified", "modified", "~"},
		{"Deleted", "deleted", "-"},
		{"Unknown", "unknown", "?"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := statusSymbol(tc.input)
			if got != tc.want {
				t.Errorf("statusSymbol(%q) = %q, want %q", tc.input, got, tc.want)
			}
		})
	}
}

// ─── tuiChooseBackup ─────────────────────────────────────────────────────────

func TestTuiChooseBackup(t *testing.T) {
	entryA := storage.BackupEntry{ID: "A"}
	entryB := storage.BackupEntry{ID: "B"}
	entries := []storage.BackupEntry{entryA, entryB}

	tests := []struct {
		name    string
		input   string
		wantID  string
		wantErr bool
	}{
		{
			name:    "EmptyUsesLatest",
			input:   "\n",
			wantID:  "A",
			wantErr: false,
		},
		{
			name:    "Numeric1ReturnsFirst",
			input:   "1\n",
			wantID:  "A",
			wantErr: false,
		},
		{
			name:    "Numeric2ReturnsSecond",
			input:   "2\n",
			wantID:  "B",
			wantErr: false,
		},
		{
			name:    "ExactIDMatch",
			input:   "B\n",
			wantID:  "B",
			wantErr: false,
		},
		{
			name:    "OutOfRange",
			input:   "5\n",
			wantID:  "",
			wantErr: true,
		},
		{
			name:    "UnknownID",
			input:   "ZZZZ\n",
			wantID:  "",
			wantErr: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			r := bufio.NewReader(strings.NewReader(tc.input))
			got, err := tuiChooseBackup(entries, r)

			if tc.wantErr {
				if err == nil {
					t.Fatalf("tuiChooseBackup(%q): expected error, got nil (entry %+v)", tc.input, got)
				}
				return
			}
			if err != nil {
				t.Fatalf("tuiChooseBackup(%q): unexpected error: %v", tc.input, err)
			}
			if got.ID != tc.wantID {
				t.Errorf("tuiChooseBackup(%q).ID = %q, want %q", tc.input, got.ID, tc.wantID)
			}
		})
	}
}
