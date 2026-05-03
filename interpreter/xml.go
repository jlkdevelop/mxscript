// xml.go — minimal XML parser and stringifier. Useful for the
// legacy APIs SaaS apps still need to integrate with: SOAP-style
// HTTP, RSS / Atom feeds, sitemaps, podcast feeds, Google Search
// Console responses, etc.
//
//   xml.parse("<root><a>1</a><b>2</b></root>")
//   // → { tag: "root", attrs: {}, children: [
//   //      { tag: "a", attrs: {}, text: "1", children: [] },
//   //      { tag: "b", attrs: {}, text: "2", children: [] },
//   //    ], text: "" }
//
//   xml.stringify({ tag: "rss", attrs: { version: "2.0" }, children: [...] })
//   // → '<rss version="2.0"><channel>...</channel></rss>'
//
// The shape mirrors what callers actually want: each node carries
// `tag`, `attrs`, `text` (the immediate text content), and
// `children`. Mixed-content elements lose ordering between text and
// children — that's an acceptable trade for the simpler API; users
// who need full fidelity should drop to `encoding/xml` directly.
package interpreter

import (
	"encoding/xml"
	"fmt"
	"strings"
)

// xml.parse(s) — turns an XML document into an MX value. Empty
// input returns null. Malformed input throws.
func builtinXMLParse(_ *Interpreter, args []Value) (Value, error) {
	if len(args) < 1 || args[0].Kind != KindString {
		return Value{}, fmt.Errorf("xml.parse(s) requires a string")
	}
	s := strings.TrimSpace(args[0].String)
	if s == "" {
		return NullValue(), nil
	}
	dec := xml.NewDecoder(strings.NewReader(s))
	for {
		tok, err := dec.Token()
		if err != nil {
			return Value{}, fmt.Errorf("xml.parse: %w", err)
		}
		if start, ok := tok.(xml.StartElement); ok {
			return parseXMLElement(dec, start)
		}
	}
}

func parseXMLElement(dec *xml.Decoder, start xml.StartElement) (Value, error) {
	out := NewOrderedMap()
	out.Set("tag", StringValue(start.Name.Local))

	attrs := NewOrderedMap()
	for _, a := range start.Attr {
		attrs.Set(a.Name.Local, StringValue(a.Value))
	}
	out.Set("attrs", ObjectValue(attrs))

	var text strings.Builder
	var children []Value
	for {
		tok, err := dec.Token()
		if err != nil {
			return Value{}, err
		}
		switch t := tok.(type) {
		case xml.StartElement:
			child, err := parseXMLElement(dec, t)
			if err != nil {
				return Value{}, err
			}
			children = append(children, child)
		case xml.CharData:
			text.WriteString(string(t))
		case xml.EndElement:
			out.Set("text", StringValue(strings.TrimSpace(text.String())))
			out.Set("children", ArrayValue(children))
			return ObjectValue(out), nil
		}
	}
}

// xml.stringify(node) — turns an MX node back into XML text. Emits
// attributes in insertion order; text content (if any) precedes
// children. Useful for building responses to SOAP / RSS consumers.
func builtinXMLStringify(_ *Interpreter, args []Value) (Value, error) {
	if len(args) < 1 || args[0].Kind != KindObject {
		return Value{}, fmt.Errorf("xml.stringify(node) requires an object")
	}
	var b strings.Builder
	if err := writeXMLNode(&b, args[0]); err != nil {
		return Value{}, err
	}
	return StringValue(b.String()), nil
}

func writeXMLNode(b *strings.Builder, v Value) error {
	if v.Kind != KindObject {
		return fmt.Errorf("xml.stringify: expected an object node, got %s", v.typeName())
	}
	tagVal, _ := v.Object.Get("tag")
	if tagVal.Kind != KindString {
		return fmt.Errorf("xml.stringify: each node must have a `tag` string")
	}
	tag := tagVal.String

	b.WriteByte('<')
	b.WriteString(tag)
	if attrs, ok := v.Object.Get("attrs"); ok && attrs.Kind == KindObject {
		for _, k := range attrs.Object.Keys {
			val, _ := attrs.Object.Get(k)
			fmt.Fprintf(b, ` %s="%s"`, k, escapeXMLAttr(val.Display()))
		}
	}
	b.WriteByte('>')

	if text, ok := v.Object.Get("text"); ok && text.Kind == KindString && text.String != "" {
		b.WriteString(escapeXMLText(text.String))
	}
	if children, ok := v.Object.Get("children"); ok && children.Kind == KindArray {
		for _, c := range children.Array {
			if err := writeXMLNode(b, c); err != nil {
				return err
			}
		}
	}

	b.WriteString("</")
	b.WriteString(tag)
	b.WriteByte('>')
	return nil
}

func escapeXMLText(s string) string {
	r := strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;")
	return r.Replace(s)
}
func escapeXMLAttr(s string) string {
	r := strings.NewReplacer("&", "&amp;", "<", "&lt;", `"`, "&quot;")
	return r.Replace(s)
}
