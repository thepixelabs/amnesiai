package cmd

import (
	"errors"
	"reflect"
	"sort"
	"testing"

	providerregistry "github.com/thepixelabs/amnesiai/internal/provider"
)

// fakeProvider is a minimal Provider used to drive presentProviderNames tests.
// Only Discover() is exercised; the rest of the interface is satisfied but
// will panic if called — none of the tests should reach them.
type fakeProvider struct {
	name      string
	discover  []string
	discoverE error
}

func (f *fakeProvider) Name() string                     { return f.name }
func (f *fakeProvider) Discover() ([]string, error)      { return f.discover, f.discoverE }
func (f *fakeProvider) Read() (map[string][]byte, error) { panic("unused in test") }
func (f *fakeProvider) Diff(map[string][]byte) ([]providerregistry.DiffEntry, error) {
	panic("unused in test")
}
func (f *fakeProvider) RestoreTo(string, map[string][]byte) error { panic("unused in test") }

func TestPresentProviderNames(t *testing.T) {
	tests := []struct {
		name    string
		names   []string
		fakes   map[string]*fakeProvider
		getErrs map[string]error
		want    []string
	}{
		{
			name:  "AllPresentReturnsAll",
			names: []string{"claude", "gemini"},
			fakes: map[string]*fakeProvider{
				"claude": {discover: []string{"/x/CLAUDE.md"}},
				"gemini": {discover: []string{"/x/GEMINI.md"}},
			},
			want: []string{"claude", "gemini"},
		},
		{
			name:  "EmptyDiscoverHidesProvider",
			names: []string{"claude", "gemini", "codex"},
			fakes: map[string]*fakeProvider{
				"claude": {discover: []string{"/x/CLAUDE.md"}},
				"gemini": {discover: nil},
				"codex":  {discover: []string{}},
			},
			want: []string{"claude"},
		},
		{
			name:  "DiscoverErrorHidesProvider",
			names: []string{"claude", "gemini"},
			fakes: map[string]*fakeProvider{
				"claude": {discover: []string{"/x/CLAUDE.md"}},
				"gemini": {discoverE: errors.New("boom")},
			},
			want: []string{"claude"},
		},
		{
			name:    "GetterErrorHidesProvider",
			names:   []string{"claude", "gemini"},
			fakes:   map[string]*fakeProvider{"claude": {discover: []string{"/x/CLAUDE.md"}}},
			getErrs: map[string]error{"gemini": errors.New("not registered")},
			want:    []string{"claude"},
		},
		{
			name:  "AllAbsentReturnsEmpty",
			names: []string{"claude", "gemini"},
			fakes: map[string]*fakeProvider{
				"claude": {discover: nil},
				"gemini": {discover: nil},
			},
			want: []string{},
		},
		{
			name:  "EmptyInputReturnsEmpty",
			names: nil,
			fakes: nil,
			want:  []string{},
		},
		{
			name:  "PreservesInputOrder",
			names: []string{"gemini", "claude", "codex"},
			fakes: map[string]*fakeProvider{
				"claude": {discover: []string{"/x/CLAUDE.md"}},
				"gemini": {discover: []string{"/x/GEMINI.md"}},
				"codex":  {discover: []string{"/x/codex.toml"}},
			},
			want: []string{"gemini", "claude", "codex"},
		},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			getter := func(n string) (providerregistry.Provider, error) {
				if err, ok := tc.getErrs[n]; ok {
					return nil, err
				}
				f, ok := tc.fakes[n]
				if !ok {
					return nil, errors.New("unknown provider " + n)
				}
				return f, nil
			}
			got := presentProviderNames(tc.names, getter)
			if got == nil {
				got = []string{}
			}
			if !reflect.DeepEqual(got, tc.want) {
				sort.Strings(got)
				sort.Strings(tc.want)
				if !reflect.DeepEqual(got, tc.want) {
					t.Fatalf("got %v, want %v", got, tc.want)
				}
			}
		})
	}
}
