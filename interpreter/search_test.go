//go:build !js

package interpreter

import (
	"path/filepath"
	"testing"
)

func newSearchDB(t *testing.T) *dbHandle {
	t.Helper()
	path := filepath.Join(t.TempDir(), "search.db")
	h, err := sqlOpen(path)
	if err != nil {
		t.Fatalf("sqlOpen: %v", err)
	}
	return h
}

func TestSearchCreateAndQuery(t *testing.T) {
	h := newSearchDB(t)
	defer h.db.Close()

	cols := ArrayValue([]Value{StringValue("title"), StringValue("body")})
	if _, err := builtinSearchCreate(nil, []Value{
		HandleValue(h), StringValue("posts_fts"), cols,
	}); err != nil {
		t.Fatalf("create: %v", err)
	}

	docs := []struct {
		id    int
		title string
		body  string
	}{
		{1, "Programming languages", "MX is fast and ergonomic"},
		{2, "Database design", "Indexing speeds up reads"},
		{3, "Async patterns", "Goroutines and channels in MX"},
	}
	for _, d := range docs {
		doc := NewOrderedMap()
		doc.Set("title", StringValue(d.title))
		doc.Set("body", StringValue(d.body))
		if _, err := builtinSearchIndex(nil, []Value{
			HandleValue(h), StringValue("posts_fts"),
			NumberValue(float64(d.id)), ObjectValue(doc),
		}); err != nil {
			t.Fatalf("index %d: %v", d.id, err)
		}
	}

	v, err := builtinSearchQuery(nil, []Value{
		HandleValue(h), StringValue("posts_fts"), StringValue("MX"),
	})
	if err != nil {
		t.Fatalf("query: %v", err)
	}
	if v.Kind != KindArray || len(v.Array) < 2 {
		t.Errorf("expected ≥2 hits for 'MX', got %d", len(v.Array))
	}
	for _, hit := range v.Array {
		id, _ := hit.Object.Get("id")
		if id.Kind != KindNumber {
			t.Errorf("hit missing id: %v", hit)
		}
	}
}

func TestSearchColumnScopedQuery(t *testing.T) {
	// FTS5's column: syntax — `title:lang` matches only when 'lang'
	// appears in the title column. Verifies our wrapper passes the
	// query through unchanged.
	h := newSearchDB(t)
	defer h.db.Close()

	cols := ArrayValue([]Value{StringValue("title"), StringValue("body")})
	builtinSearchCreate(nil, []Value{HandleValue(h), StringValue("docs"), cols})

	add := func(id int, title, body string) {
		doc := NewOrderedMap()
		doc.Set("title", StringValue(title))
		doc.Set("body", StringValue(body))
		builtinSearchIndex(nil, []Value{
			HandleValue(h), StringValue("docs"),
			NumberValue(float64(id)), ObjectValue(doc),
		})
	}
	add(1, "Programming languages", "MX is fast")
	add(2, "Tutorial", "Learn programming with MX")

	v, _ := builtinSearchQuery(nil, []Value{
		HandleValue(h), StringValue("docs"), StringValue(`title:programming`),
	})
	// Only doc 1 mentions "programming" in the title.
	if len(v.Array) != 1 {
		t.Errorf("expected 1 hit (title-scoped), got %d", len(v.Array))
	}
}

func TestSearchDelete(t *testing.T) {
	h := newSearchDB(t)
	defer h.db.Close()

	cols := ArrayValue([]Value{StringValue("body")})
	builtinSearchCreate(nil, []Value{HandleValue(h), StringValue("notes"), cols})
	doc := NewOrderedMap()
	doc.Set("body", StringValue("hello world"))
	builtinSearchIndex(nil, []Value{
		HandleValue(h), StringValue("notes"), NumberValue(1), ObjectValue(doc),
	})

	v, _ := builtinSearchQuery(nil, []Value{HandleValue(h), StringValue("notes"), StringValue("hello")})
	if len(v.Array) != 1 {
		t.Fatalf("expected 1 hit before delete, got %d", len(v.Array))
	}
	builtinSearchDelete(nil, []Value{HandleValue(h), StringValue("notes"), NumberValue(1)})
	v, _ = builtinSearchQuery(nil, []Value{HandleValue(h), StringValue("notes"), StringValue("hello")})
	if len(v.Array) != 0 {
		t.Errorf("expected 0 hits after delete, got %d", len(v.Array))
	}
}

func TestSearchReindexIsIdempotent(t *testing.T) {
	h := newSearchDB(t)
	defer h.db.Close()

	cols := ArrayValue([]Value{StringValue("body")})
	builtinSearchCreate(nil, []Value{HandleValue(h), StringValue("t"), cols})
	for k := 0; k < 3; k++ {
		doc := NewOrderedMap()
		doc.Set("body", StringValue("foo bar baz"))
		builtinSearchIndex(nil, []Value{
			HandleValue(h), StringValue("t"), NumberValue(1), ObjectValue(doc),
		})
	}
	v, _ := builtinSearchQuery(nil, []Value{HandleValue(h), StringValue("t"), StringValue("foo")})
	if len(v.Array) != 1 {
		t.Errorf("re-indexing should not duplicate; got %d hits", len(v.Array))
	}
}
