// Package toon は値を TOON (Token-Oriented Object Notation, https://github.com/toon-format/toon) で書き出す。
// JSON に書き出せる値なら、JSON のキーの順のまま書く
package toon

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"strconv"
	"strings"
)

type field struct {
	key   string
	value any
}

type object []field

func Marshal(v any) ([]byte, error) {
	b, err := json.Marshal(v)
	if err != nil {
		return nil, err
	}
	dec := json.NewDecoder(bytes.NewReader(b))
	dec.UseNumber()
	tree, err := decode(dec)
	if err != nil {
		return nil, err
	}
	var w strings.Builder
	switch t := tree.(type) {
	case object:
		writeObject(&w, t, 0)
	case []any:
		writeArray(&w, "", t, 0)
	default:
		w.WriteString(primitive(t))
		w.WriteByte('\n')
	}
	return []byte(w.String()), nil
}

func decode(dec *json.Decoder) (any, error) {
	tok, err := dec.Token()
	if err != nil {
		return nil, err
	}
	switch t := tok.(type) {
	case json.Delim:
		switch t {
		case '{':
			var o object
			for dec.More() {
				k, err := dec.Token()
				if err != nil {
					return nil, err
				}
				v, err := decode(dec)
				if err != nil {
					return nil, err
				}
				o = append(o, field{key: k.(string), value: v})
			}
			_, err := dec.Token()
			return o, err
		case '[':
			a := []any{}
			for dec.More() {
				v, err := decode(dec)
				if err != nil {
					return nil, err
				}
				a = append(a, v)
			}
			_, err := dec.Token()
			return a, err
		}
		return nil, io.ErrUnexpectedEOF
	default:
		return t, nil
	}
}

func indent(w *strings.Builder, depth int) {
	w.WriteString(strings.Repeat("  ", depth))
}

func writeObject(w *strings.Builder, o object, depth int) {
	for _, f := range o {
		switch v := f.value.(type) {
		case object:
			indent(w, depth)
			w.WriteString(key(f.key) + ":\n")
			writeObject(w, v, depth+1)
		case []any:
			writeArray(w, key(f.key), v, depth)
		default:
			indent(w, depth)
			w.WriteString(key(f.key) + ": " + primitive(v) + "\n")
		}
	}
}

func writeArray(w *strings.Builder, name string, a []any, depth int) {
	indent(w, depth)
	header := fmt.Sprintf("%s[%d]", name, len(a))
	if allPrimitive(a) {
		vals := make([]string, len(a))
		for i, v := range a {
			vals[i] = primitive(v)
		}
		w.WriteString(header + ": " + strings.Join(vals, ",") + "\n")
		return
	}
	if keys, ok := tabular(a); ok {
		qs := make([]string, len(keys))
		for i, k := range keys {
			qs[i] = key(k)
		}
		w.WriteString(header + "{" + strings.Join(qs, ",") + "}:\n")
		for _, row := range a {
			vals := make([]string, len(keys))
			for i, f := range row.(object) {
				vals[i] = primitive(f.value)
			}
			indent(w, depth+1)
			w.WriteString(strings.Join(vals, ",") + "\n")
		}
		return
	}
	w.WriteString(header + ":\n")
	for _, item := range a {
		switch v := item.(type) {
		case object:
			if len(v) == 0 {
				indent(w, depth+1)
				w.WriteString("-\n")
				continue
			}
			var first strings.Builder
			writeObject(&first, v[:1], 0)
			indent(w, depth+1)
			w.WriteString("- " + first.String())
			writeObject(w, v[1:], depth+2)
		case []any:
			var sub strings.Builder
			writeArray(&sub, "", v, 0)
			lines := strings.SplitAfter(strings.TrimSuffix(sub.String(), "\n"), "\n")
			indent(w, depth+1)
			w.WriteString("- " + lines[0])
			for _, l := range lines[1:] {
				indent(w, depth+2)
				w.WriteString(l)
			}
			w.WriteString("\n")
		default:
			indent(w, depth+1)
			w.WriteString("- " + primitive(v) + "\n")
		}
	}
}

func allPrimitive(a []any) bool {
	for _, v := range a {
		switch v.(type) {
		case object, []any:
			return false
		}
	}
	return true
}

func tabular(a []any) ([]string, bool) {
	if len(a) == 0 {
		return nil, false
	}
	first, ok := a[0].(object)
	if !ok || len(first) == 0 {
		return nil, false
	}
	keys := make([]string, len(first))
	for i, f := range first {
		keys[i] = f.key
	}
	for _, row := range a {
		o, ok := row.(object)
		if !ok || len(o) != len(keys) {
			return nil, false
		}
		for i, f := range o {
			if f.key != keys[i] {
				return nil, false
			}
			switch f.value.(type) {
			case object, []any:
				return nil, false
			}
		}
	}
	return keys, true
}

func key(k string) string {
	if k != "" && strings.IndexFunc(k, func(r rune) bool {
		return !(r == '_' || r == '.' || r == '-' || r >= '0' && r <= '9' || r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z')
	}) < 0 && !(k[0] >= '0' && k[0] <= '9') {
		return k
	}
	return strconv.Quote(k)
}

func primitive(v any) string {
	switch t := v.(type) {
	case nil:
		return "null"
	case bool:
		return strconv.FormatBool(t)
	case json.Number:
		return t.String()
	case string:
		if needsQuote(t) {
			return strconv.Quote(t)
		}
		return t
	}
	return fmt.Sprint(v)
}

func needsQuote(s string) bool {
	if s == "" || s != strings.TrimSpace(s) || s == "true" || s == "false" || s == "null" || strings.HasPrefix(s, "-") {
		return true
	}
	if _, err := strconv.ParseFloat(s, 64); err == nil {
		return true
	}
	return strings.ContainsAny(s, ",:\"\\[]{}\n\r\t#")
}
