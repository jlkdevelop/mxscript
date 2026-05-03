package pkg

import (
	"path/filepath"
	"testing"
)

func TestNormalizeImportPath(t *testing.T) {
	cases := map[string]string{
		"github.com/foo/bar":                    "github.com/foo/bar",
		"https://github.com/foo/bar":            "github.com/foo/bar",
		"https://github.com/foo/bar.git":        "github.com/foo/bar",
		"git@github.com:foo/bar.git":            "github.com/foo/bar",
		"  github.com/foo/bar  ":                "github.com/foo/bar",
		"http://example.org/owner/repo":         "example.org/owner/repo",
		"https://gitlab.example.com/team/x.git": "gitlab.example.com/team/x",
	}
	for input, want := range cases {
		got := NormalizeImportPath(input)
		if got != want {
			t.Errorf("%q: got %q, want %q", input, got, want)
		}
	}
}

func TestCloneURL(t *testing.T) {
	if got := CloneURL("github.com/foo/bar"); got != "https://github.com/foo/bar.git" {
		t.Errorf("got %q", got)
	}
}

func TestLocalPathStripsHost(t *testing.T) {
	got := LocalPath(".", "github.com/foo/bar")
	want := filepath.Join(".", "mx_modules", "foo/bar")
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestEntryFileDefaultsToMainMx(t *testing.T) {
	dep := Dependency{URL: "github.com/foo/bar"}
	got := EntryFile(".", dep, "github.com/foo/bar")
	if filepath.Base(got) != "main.mx" {
		t.Errorf("entry: got %q", got)
	}
}

func TestEntryFileRespectsManifestOverride(t *testing.T) {
	dep := Dependency{URL: "github.com/foo/bar", Entry: "lib.mx"}
	got := EntryFile(".", dep, "github.com/foo/bar")
	if filepath.Base(got) != "lib.mx" {
		t.Errorf("entry: got %q", got)
	}
}

func TestResolveImportFileSkipsRelativePaths(t *testing.T) {
	// Relative imports (`./auth.mx`, `helpers.mx`) must not be
	// resolved as packages — the host has no dot in the first
	// segment so we return "" and let the runtime use the original
	// relative-path logic.
	cases := []string{"./auth.mx", "auth.mx", "../shared/util.mx"}
	for _, p := range cases {
		if got := ResolveImportFile(".", p); got != "" {
			t.Errorf("%q: expected empty (relative), got %q", p, got)
		}
	}
}

func TestResolveImportFileResolvesPackages(t *testing.T) {
	// Package paths return a valid file path even when the manifest
	// is missing (so users can write `import "github.com/foo/bar"`
	// before running `mx pkg add`).
	got := ResolveImportFile(".", "github.com/foo/bar")
	if got == "" {
		t.Error("expected a path for github.com/foo/bar")
	}
	if filepath.Base(got) != "main.mx" {
		t.Errorf("got %q, want a main.mx file", got)
	}
}

func TestSaveAndLoadManifest(t *testing.T) {
	dir := t.TempDir()
	m := &Manifest{
		Name:    "demo",
		Version: "0.1.0",
		Dependencies: map[string]Dependency{
			"github.com/foo/bar": {URL: "github.com/foo/bar", Ref: "abc123"},
		},
	}
	if err := SaveManifest(dir, m); err != nil {
		t.Fatalf("save: %v", err)
	}
	got, err := LoadManifest(dir)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if got.Name != "demo" || got.Dependencies["github.com/foo/bar"].Ref != "abc123" {
		t.Errorf("roundtrip mismatch: %+v", got)
	}
}

func TestLoadManifestMissingReturnsNil(t *testing.T) {
	dir := t.TempDir()
	m, err := LoadManifest(dir)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if m != nil {
		t.Errorf("expected nil for missing manifest, got %+v", m)
	}
}

func TestInitOnExistingManifestReturnsFalse(t *testing.T) {
	dir := t.TempDir()
	created, err := Init(dir, "first")
	if err != nil {
		t.Fatal(err)
	}
	if !created {
		t.Error("first init should create")
	}
	created, err = Init(dir, "second")
	if err != nil {
		t.Fatal(err)
	}
	if created {
		t.Error("second init should report not-created")
	}
	// Original name preserved.
	m, _ := LoadManifest(dir)
	if m.Name != "first" {
		t.Errorf("name: got %q, want first", m.Name)
	}
}
