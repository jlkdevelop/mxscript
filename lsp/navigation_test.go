package lsp

import "testing"

const navSrc = `let DB_PATH = "./data.db"

fn make_user(name) {
  return { id: 1, name: name }
}

middleware require_auth {
  if (claims == null) { return status(401, {}) }
}

get /users {
  use require_auth
  let u = make_user("jassim")
  return json(u)
}

post /users {
  use require_auth
  return status(201, {})
}
`

func TestFindDefinitionFn(t *testing.T) {
	line, col, ok := findDefinition(navSrc, "make_user")
	if !ok {
		t.Fatal("expected to find make_user")
	}
	if line != 2 {
		t.Errorf("line: got %d, want 2", line)
	}
	if col < 3 {
		t.Errorf("col should point at name, got %d", col)
	}
}

func TestFindDefinitionLet(t *testing.T) {
	line, _, ok := findDefinition(navSrc, "DB_PATH")
	if !ok || line != 0 {
		t.Errorf("DB_PATH: got line=%d ok=%v", line, ok)
	}
}

func TestFindDefinitionMiddleware(t *testing.T) {
	line, _, ok := findDefinition(navSrc, "require_auth")
	if !ok {
		t.Fatal("expected to find require_auth")
	}
	if line != 6 {
		t.Errorf("line: got %d, want 6", line)
	}
}

func TestFindDefinitionPrefixSafety(t *testing.T) {
	// `let foo` should NOT match a query for `fo` — whole-word.
	line, _, ok := findDefinition("let foobar = 1\n", "foo")
	if ok {
		t.Errorf("expected no match (foo vs foobar), got line=%d", line)
	}
}

func TestFindDefinitionMissing(t *testing.T) {
	if _, _, ok := findDefinition(navSrc, "nonexistent"); ok {
		t.Error("expected miss")
	}
}

func TestFindReferencesAllSites(t *testing.T) {
	refs := findReferences(navSrc, "require_auth", "file:///x.mx")
	// One in the middleware decl + two `use` sites = 3.
	if len(refs) != 3 {
		t.Errorf("got %d refs, want 3", len(refs))
	}
}

func TestFindReferencesWholeWord(t *testing.T) {
	src := "let user = 1\nlet username = 2\nprint(user)\n"
	refs := findReferences(src, "user", "x.mx")
	// Only the two `user` occurrences (decl + print) — `username`
	// must not match.
	if len(refs) != 2 {
		t.Errorf("got %d refs, want 2 (got matches in 'username'?)", len(refs))
	}
}

func TestDocumentSymbolsExtractsAllDecls(t *testing.T) {
	syms := documentSymbols(navSrc)
	names := map[string]bool{}
	for _, s := range syms {
		m := s.(map[string]any)
		names[m["name"].(string)] = true
	}
	for _, want := range []string{"DB_PATH", "make_user", "require_auth"} {
		if !names[want] {
			t.Errorf("missing symbol %q in outline %v", want, names)
		}
	}
	// Routes should appear with method + path verbatim.
	hasGet := false
	hasPost := false
	for n := range names {
		if n == "get /users" {
			hasGet = true
		}
		if n == "post /users" {
			hasPost = true
		}
	}
	if !hasGet || !hasPost {
		t.Errorf("expected get + post routes in outline, got %v", names)
	}
}
