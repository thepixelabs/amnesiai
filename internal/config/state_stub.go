// Package config — state_stub.go
//
// TODO: Replace this stub with Track E's state.go once it is merged.
// Track E owns the full State schema, LoadState, and Save logic.
// This minimal stub provides only the surface that Track F needs so
// that git-remote multi-account binding compiles cleanly without
// duplicating Track E's schema.
package config

// BindRemote records that a specific host/account pair is associated with
// the given repo URL.  The real implementation will persist this in
// ~/.amnesiai/state.json; the stub is a no-op.
func BindRemote(repoURL, host, account string) error {
	return nil
}
