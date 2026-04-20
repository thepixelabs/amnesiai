package storage_test

import (
	"testing"

	"github.com/thepixelabs/amnesiai/internal/storage"
)

// TestNew_ModeRouting verifies that storage.New returns the correct
// implementation or error for each supported and unsupported mode.
func TestNew_ModeRouting(t *testing.T) {
	tests := []struct {
		name         string
		mode         string
		wantNilStore bool
		wantErr      bool
	}{
		{
			name:         "LocalReturnsStorage",
			mode:         "local",
			wantNilStore: false,
			wantErr:      false,
		},
		{
			name:         "GitLocalReturnsStorage",
			mode:         "git-local",
			wantNilStore: false,
			wantErr:      false,
		},
		{
			name:         "GitRemoteReturnsStorage",
			mode:         "git-remote",
			wantNilStore: false,
			wantErr:      false,
		},
		{
			name:         "UnknownReturnsError",
			mode:         "s3",
			wantNilStore: true,
			wantErr:      true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			s, err := storage.New(tc.mode, dir)

			if tc.wantErr {
				if err == nil {
					t.Fatalf("storage.New(%q): expected error, got nil (store=%v)", tc.mode, s)
				}
			} else {
				if err != nil {
					t.Fatalf("storage.New(%q): unexpected error: %v", tc.mode, err)
				}
			}

			if tc.wantNilStore && s != nil {
				t.Errorf("storage.New(%q): expected nil store, got %v", tc.mode, s)
			}
			if !tc.wantNilStore && s == nil {
				t.Errorf("storage.New(%q): expected non-nil store, got nil", tc.mode)
			}
		})
	}
}
