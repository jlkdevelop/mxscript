// pkg implements MX Script's package manager. The model is
// deliberately Go-flavoured: dependency identifiers are full URLs
// (`github.com/owner/repo`), there's a tiny human-readable manifest
// (`mxpkg.json`), and downloaded sources live under `./mx_modules`
// in the project root. There is no central registry — packages are
// fetched directly from their git origin via `git clone`.
//
// Today the manifest tracks: dependency URL, locked commit SHA, and
// optional entry-point file (defaults to `main.mx`). Future versions
// will add semver / tag support, a lockfile separate from the
// manifest, and `mx pkg vendor` for offline builds.
package pkg

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
)

const (
	ManifestFile = "mxpkg.json"
	ModulesDir   = "mx_modules"
)

// Manifest is the JSON shape persisted at the project root. Keep
// fields stable — third-party tools (CI, registries) read it.
type Manifest struct {
	Name         string                `json:"name"`
	Version      string                `json:"version"`
	Description  string                `json:"description,omitempty"`
	Dependencies map[string]Dependency `json:"dependencies,omitempty"`
}

// Dependency is one entry in the manifest's `dependencies` map. The
// key is the import path (e.g. `github.com/foo/bar`); the value
// captures everything we need to reproduce an install: the locked
// commit SHA, and the entry file the import statement resolves to.
type Dependency struct {
	URL   string `json:"url"`
	Ref   string `json:"ref"`
	Entry string `json:"entry,omitempty"` // defaults to main.mx
}

// LoadManifest reads mxpkg.json from `dir` (or `.` if dir is empty).
// Returns nil + nil if the file doesn't exist — callers decide
// whether that's an error in their context.
func LoadManifest(dir string) (*Manifest, error) {
	path := filepath.Join(dir, ManifestFile)
	raw, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var m Manifest
	if err := json.Unmarshal(raw, &m); err != nil {
		return nil, fmt.Errorf("parse %s: %w", path, err)
	}
	return &m, nil
}

// SaveManifest writes the manifest with stable key ordering and
// two-space indentation so diffs stay readable across versions.
func SaveManifest(dir string, m *Manifest) error {
	if m.Dependencies == nil {
		m.Dependencies = map[string]Dependency{}
	}
	raw, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return err
	}
	raw = append(raw, '\n')
	return os.WriteFile(filepath.Join(dir, ManifestFile), raw, 0o644)
}

// Init creates a fresh manifest if one doesn't exist. Returns true if
// it created the file, false if it already existed.
func Init(dir, name string) (bool, error) {
	if existing, _ := LoadManifest(dir); existing != nil {
		return false, nil
	}
	m := &Manifest{
		Name:         name,
		Version:      "0.1.0",
		Dependencies: map[string]Dependency{},
	}
	return true, SaveManifest(dir, m)
}

// NormalizeImportPath turns user-friendly forms into the canonical
// host/owner/repo path used as the manifest key. Accepts:
//
//	github.com/foo/bar
//	https://github.com/foo/bar
//	https://github.com/foo/bar.git
//	git@github.com:foo/bar.git
func NormalizeImportPath(raw string) string {
	s := strings.TrimSpace(raw)
	s = strings.TrimSuffix(s, ".git")
	switch {
	case strings.HasPrefix(s, "https://"):
		return strings.TrimPrefix(s, "https://")
	case strings.HasPrefix(s, "http://"):
		return strings.TrimPrefix(s, "http://")
	case strings.HasPrefix(s, "git@"):
		// git@github.com:foo/bar -> github.com/foo/bar
		s = strings.TrimPrefix(s, "git@")
		return strings.Replace(s, ":", "/", 1)
	}
	return s
}

// CloneURL builds the HTTPS clone URL for an import path so the
// package manager can shell out to `git clone` without baking in any
// assumptions about a specific host beyond "https works".
func CloneURL(importPath string) string {
	return "https://" + importPath + ".git"
}

// LocalPath returns where a dependency lives on disk, relative to the
// project root.
//
//	github.com/foo/bar -> mx_modules/foo/bar  (host stripped — repos are namespaced
//	                                            by owner/name, not by host, today)
//
// The host segment is omitted intentionally so paths in source code
// stay short. If multi-host support becomes a problem we'll reverse
// this — for now the design matches Go's "repo by owner/name" mental
// model.
func LocalPath(dir, importPath string) string {
	parts := strings.SplitN(importPath, "/", 2)
	if len(parts) < 2 {
		return filepath.Join(dir, ModulesDir, importPath)
	}
	return filepath.Join(dir, ModulesDir, parts[1])
}

// EntryFile returns the absolute path to the entry .mx file for a
// dependency. Defaults to main.mx if the manifest doesn't override.
func EntryFile(dir string, dep Dependency, importPath string) string {
	entry := dep.Entry
	if entry == "" {
		entry = "main.mx"
	}
	return filepath.Join(LocalPath(dir, importPath), entry)
}

// Add clones (or reuses) a dependency, locks it to the current HEAD
// commit, and updates the manifest. If the dependency already exists
// in mx_modules we don't re-clone — the caller can `mx pkg update`
// for that.
func Add(dir, rawPath string) (Dependency, error) {
	importPath := NormalizeImportPath(rawPath)
	if !strings.Contains(importPath, "/") {
		return Dependency{}, fmt.Errorf("invalid import path %q (expected host/owner/repo)", rawPath)
	}
	local := LocalPath(dir, importPath)
	if _, err := os.Stat(local); os.IsNotExist(err) {
		if err := os.MkdirAll(filepath.Dir(local), 0o755); err != nil {
			return Dependency{}, err
		}
		cmd := exec.Command("git", "clone", "--depth=1", CloneURL(importPath), local)
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
		if err := cmd.Run(); err != nil {
			return Dependency{}, fmt.Errorf("git clone: %w", err)
		}
	}
	ref, err := headSHA(local)
	if err != nil {
		return Dependency{}, err
	}
	dep := Dependency{
		URL: importPath,
		Ref: ref,
	}
	manifest, err := LoadManifest(dir)
	if err != nil {
		return dep, err
	}
	if manifest == nil {
		manifest = &Manifest{
			Name:         filepath.Base(absOrSelf(dir)),
			Version:      "0.1.0",
			Dependencies: map[string]Dependency{},
		}
	}
	if manifest.Dependencies == nil {
		manifest.Dependencies = map[string]Dependency{}
	}
	manifest.Dependencies[importPath] = dep
	return dep, SaveManifest(dir, manifest)
}

// Remove deletes the on-disk module directory and the manifest entry.
// Doesn't `git rm` because mx_modules is expected to be in .gitignore.
func Remove(dir, rawPath string) error {
	importPath := NormalizeImportPath(rawPath)
	manifest, err := LoadManifest(dir)
	if err != nil {
		return err
	}
	if manifest == nil {
		return fmt.Errorf("no %s in %s", ManifestFile, dir)
	}
	if _, ok := manifest.Dependencies[importPath]; !ok {
		return fmt.Errorf("%s not in dependencies", importPath)
	}
	delete(manifest.Dependencies, importPath)
	if err := SaveManifest(dir, manifest); err != nil {
		return err
	}
	return os.RemoveAll(LocalPath(dir, importPath))
}

// Update pulls the latest commit for a single dependency and updates
// its locked Ref. With empty path, updates every dependency.
func Update(dir, rawPath string) error {
	manifest, err := LoadManifest(dir)
	if err != nil {
		return err
	}
	if manifest == nil {
		return fmt.Errorf("no %s in %s", ManifestFile, dir)
	}
	targets := []string{}
	if rawPath != "" {
		targets = append(targets, NormalizeImportPath(rawPath))
	} else {
		for k := range manifest.Dependencies {
			targets = append(targets, k)
		}
		sort.Strings(targets)
	}
	for _, p := range targets {
		dep, ok := manifest.Dependencies[p]
		if !ok {
			return fmt.Errorf("%s not in dependencies", p)
		}
		local := LocalPath(dir, p)
		// Pull latest then re-record the SHA.
		pull := exec.Command("git", "-C", local, "pull", "--ff-only")
		pull.Stdout = os.Stdout
		pull.Stderr = os.Stderr
		if err := pull.Run(); err != nil {
			return fmt.Errorf("git pull %s: %w", p, err)
		}
		ref, err := headSHA(local)
		if err != nil {
			return err
		}
		dep.Ref = ref
		manifest.Dependencies[p] = dep
	}
	return SaveManifest(dir, manifest)
}

// Install ensures every manifest dependency is present on disk. Used
// by collaborators after `git clone`-ing a project. Skips packages
// that are already installed at the locked SHA.
func Install(dir string) error {
	manifest, err := LoadManifest(dir)
	if err != nil {
		return err
	}
	if manifest == nil {
		return fmt.Errorf("no %s in %s", ManifestFile, dir)
	}
	keys := make([]string, 0, len(manifest.Dependencies))
	for k := range manifest.Dependencies {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, p := range keys {
		dep := manifest.Dependencies[p]
		local := LocalPath(dir, p)
		if _, err := os.Stat(local); err == nil {
			// Already cloned — check its SHA against the lock.
			ref, _ := headSHA(local)
			if ref == dep.Ref {
				continue
			}
		}
		if err := os.MkdirAll(filepath.Dir(local), 0o755); err != nil {
			return err
		}
		clone := exec.Command("git", "clone", CloneURL(p), local)
		clone.Stdout = os.Stdout
		clone.Stderr = os.Stderr
		if err := clone.Run(); err != nil {
			return fmt.Errorf("git clone %s: %w", p, err)
		}
		// Hard-reset to the locked commit so the install is reproducible.
		reset := exec.Command("git", "-C", local, "reset", "--hard", dep.Ref)
		reset.Stdout = os.Stdout
		reset.Stderr = os.Stderr
		if err := reset.Run(); err != nil {
			return fmt.Errorf("git reset %s@%s: %w", p, dep.Ref, err)
		}
	}
	return nil
}

// headSHA returns `git rev-parse HEAD` for the given checkout.
func headSHA(repoDir string) (string, error) {
	out, err := exec.Command("git", "-C", repoDir, "rev-parse", "HEAD").Output()
	if err != nil {
		return "", fmt.Errorf("rev-parse: %w", err)
	}
	return strings.TrimSpace(string(out)), nil
}

func absOrSelf(p string) string {
	abs, err := filepath.Abs(p)
	if err != nil {
		return p
	}
	return abs
}

// ResolveImportFile, given an import path that looks like a package
// reference (`github.com/foo/bar`), returns the on-disk .mx file the
// MX runtime should read. Returns "" when the path doesn't look like
// a package reference (so callers can fall back to plain file paths).
func ResolveImportFile(dir, importPath string) string {
	if !looksLikePackagePath(importPath) {
		return ""
	}
	manifest, _ := LoadManifest(dir)
	if manifest != nil {
		if dep, ok := manifest.Dependencies[importPath]; ok {
			return EntryFile(dir, dep, importPath)
		}
	}
	// No manifest entry: fall back to the conventional layout so
	// `import "github.com/foo/bar"` works even before `mx pkg add`.
	return filepath.Join(LocalPath(dir, importPath), "main.mx")
}

// looksLikePackagePath returns true for `host.tld/owner/repo` style
// paths and false for anything that looks like a relative file
// reference. We must not misclassify `auth.mx` (relative file in CWD)
// as a package just because it has a dot.
func looksLikePackagePath(p string) bool {
	if strings.HasPrefix(p, "./") || strings.HasPrefix(p, "../") || strings.HasPrefix(p, "/") {
		return false
	}
	if strings.HasSuffix(p, ".mx") {
		return false
	}
	idx := strings.Index(p, "/")
	if idx < 0 {
		return false
	}
	first := p[:idx]
	// Need at least one dot in the host segment for it to plausibly
	// be a domain, and the remaining path needs at least one more
	// slash so we have host/owner/repo (or deeper).
	if !strings.Contains(first, ".") {
		return false
	}
	rest := p[idx+1:]
	return strings.Contains(rest, "/")
}
