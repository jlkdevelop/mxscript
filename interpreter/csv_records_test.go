package interpreter

import (
	"strings"
	"testing"
)

func TestCSVRecordsParsesHeader(t *testing.T) {
	src := `name,email,age
Jassim,j@example.com,30
Ada,a@example.com,28`
	v, err := builtinCSVRecords(nil, []Value{StringValue(src)})
	if err != nil {
		t.Fatalf("records: %v", err)
	}
	if v.Kind != KindArray || len(v.Array) != 2 {
		t.Fatalf("got %+v", v)
	}
	first := v.Array[0].Object
	name, _ := first.Get("name")
	email, _ := first.Get("email")
	age, _ := first.Get("age")
	if name.String != "Jassim" || email.String != "j@example.com" || age.String != "30" {
		t.Errorf("first row: %v %v %v", name, email, age)
	}
}

func TestCSVRecordsTolerantOfShortRows(t *testing.T) {
	// Last row missing the email column — should treat as empty string.
	src := `name,email
Ada,a@example.com
Bob`
	v, err := builtinCSVRecords(nil, []Value{StringValue(src)})
	if err != nil {
		t.Fatalf("records: %v", err)
	}
	bob := v.Array[1].Object
	email, _ := bob.Get("email")
	if email.String != "" {
		t.Errorf("missing email: got %q", email.String)
	}
	name, _ := bob.Get("name")
	if name.String != "Bob" {
		t.Errorf("name: got %v", name)
	}
}

func TestCSVRecordsEmptyInput(t *testing.T) {
	v, err := builtinCSVRecords(nil, []Value{StringValue("")})
	if err != nil {
		t.Fatalf("records: %v", err)
	}
	if v.Kind != KindArray || len(v.Array) != 0 {
		t.Errorf("got %+v", v)
	}
}

func TestCSVWriteRecordsRoundtrip(t *testing.T) {
	row1 := NewOrderedMap()
	row1.Set("name", StringValue("Jassim"))
	row1.Set("email", StringValue("j@example.com"))
	row2 := NewOrderedMap()
	row2.Set("name", StringValue("Ada"))
	row2.Set("email", StringValue("a@example.com"))

	csvOut, err := builtinCSVWriteRecords(nil, []Value{
		ArrayValue([]Value{ObjectValue(row1), ObjectValue(row2)}),
	})
	if err != nil {
		t.Fatalf("write: %v", err)
	}
	// Header order from the first row, so "name,email" not alphabetical.
	if !strings.HasPrefix(csvOut.String, "name,email\n") {
		t.Errorf("expected name,email header, got %q", csvOut.String)
	}
	if !strings.Contains(csvOut.String, "Jassim,j@example.com") {
		t.Errorf("missing Jassim row: %q", csvOut.String)
	}

	// Round-trip: parse what we wrote and confirm we get equivalent data.
	parsed, _ := builtinCSVRecords(nil, []Value{csvOut})
	if len(parsed.Array) != 2 {
		t.Fatalf("roundtrip: got %d rows", len(parsed.Array))
	}
	first := parsed.Array[0].Object
	if name, _ := first.Get("name"); name.String != "Jassim" {
		t.Errorf("roundtrip name: %v", name)
	}
}

func TestCSVWriteRecordsEscapesEmbeddedComma(t *testing.T) {
	row := NewOrderedMap()
	row.Set("name", StringValue(`Jassim, "Hammer"`))
	row.Set("note", StringValue("uses comma"))
	out, _ := builtinCSVWriteRecords(nil, []Value{ArrayValue([]Value{ObjectValue(row)})})
	// The csv writer should quote the value containing comma + quote.
	if !strings.Contains(out.String, `"Jassim, ""Hammer"""`) {
		t.Errorf("quoting wrong: %q", out.String)
	}
}
