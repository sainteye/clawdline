// Command contract-gen turns api/v1/*.schema.json into the types every side of
// this system speaks: Go for the daemon, TypeScript for the console and for the
// phone app that will read the same routes.
//
// It exists because the alternative already happened twice. The Swift app hand
// assembles its payloads and holds its contract in prose, so a field can change
// in one place and stay right everywhere else that describes it. The first Go
// handlers repeated that, at forty map literals. Neither side was wrong on
// purpose; there was simply nothing that could notice.
//
// So this generator is the mechanism, and `-check` is the thing that notices.
//
// The subset of JSON Schema it reads is deliberately small: named definitions,
// objects, string enums, arrays, and refs. An inline object is refused rather
// than given a synthesised name, because a type worth sending is a type worth
// naming.
package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"go/format"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

type schema struct {
	Ref         string             `json:"$ref"`
	Type        string             `json:"type"`
	Description string             `json:"description"`
	Enum        []string           `json:"enum"`
	Properties  map[string]*schema `json:"properties"`
	Required    []string           `json:"required"`
	Items       *schema            `json:"items"`
	// Nullable says the key is always present and its value may be null. That
	// is a different promise from an optional key, which may be absent
	// entirely, and a client written against one breaks on the other.
	Nullable bool `json:"nullable"`
}

type schemaFile struct {
	ID          string             `json:"$id"`
	Title       string             `json:"title"`
	Description string             `json:"description"`
	Defs        map[string]*schema `json:"$defs"`
}

type def struct {
	name   string
	origin string
	s      *schema
}

var defs = map[string]*def{}

func main() {
	root := "."
	check := false
	for _, a := range os.Args[1:] {
		if a == "-check" {
			check = true
		} else {
			root = a
		}
	}

	paths, err := filepath.Glob(filepath.Join(root, "api", "v1", "*.schema.json"))
	if err != nil || len(paths) == 0 {
		// A zero-file run is a failure, not a pass. A generator that silently
		// produces nothing looks exactly like a generator with nothing to do.
		fatal("no schemas found under %s/api/v1", root)
	}
	sort.Strings(paths)

	for _, p := range paths {
		raw, err := os.ReadFile(p)
		if err != nil {
			fatal("%s: %v", p, err)
		}
		var f schemaFile
		if err := json.Unmarshal(raw, &f); err != nil {
			fatal("%s: %v", p, err)
		}
		for name, s := range f.Defs {
			if prior, dup := defs[name]; dup {
				fatal("%s defines %s, which %s already defines", filepath.Base(p), name, prior.origin)
			}
			defs[name] = &def{name: name, origin: filepath.Base(p), s: s}
		}
	}

	names := make([]string, 0, len(defs))
	for n := range defs {
		names = append(names, n)
	}
	sort.Strings(names)

	want := map[string][]byte{
		filepath.Join(root, "internal", "contract", "zz_generated.go"): renderGo(names),
		filepath.Join(root, "web", "contract", "src", "generated.ts"):  renderTS(names),
	}

	if check {
		drift := []string{}
		for path, body := range want {
			have, err := os.ReadFile(path)
			if err != nil || !bytes.Equal(have, body) {
				drift = append(drift, path)
			}
		}
		sort.Strings(drift)
		if len(drift) > 0 {
			for _, d := range drift {
				fmt.Fprintf(os.Stderr, "out of date: %s\n", d)
			}
			fatal("%d generated file(s) do not match api/v1; run: go run ./tools/contract-gen", len(drift))
		}
		fmt.Printf("contract: %d types, %d generated files current\n", len(names), len(want))
		return
	}

	for path, body := range want {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			fatal("%v", err)
		}
		if err := os.WriteFile(path, body, 0o644); err != nil {
			fatal("%v", err)
		}
	}
	fmt.Printf("contract: %d types -> %d files\n", len(names), len(want))
}

func fatal(format string, a ...any) {
	fmt.Fprintf(os.Stderr, "contract-gen: "+format+"\n", a...)
	os.Exit(1)
}

// ---------- shared shape questions ----------

func isEnum(s *schema) bool { return len(s.Enum) > 0 }

func refName(ref string) string {
	if i := strings.LastIndex(ref, "/"); i >= 0 {
		return ref[i+1:]
	}
	return ref
}

func target(s *schema) *def {
	if s.Ref == "" {
		return nil
	}
	d, ok := defs[refName(s.Ref)]
	if !ok {
		fatal("unknown $ref %q", s.Ref)
	}
	return d
}

// sortedProps returns property names in one fixed order so that both languages,
// and every rerun, agree. Alphabetical is also the order Go's encoder uses for
// a map, which is what the handlers emitted before these types existed: the
// migration is therefore byte-for-byte checkable against the old output.
func sortedProps(s *schema) []string {
	out := make([]string, 0, len(s.Properties))
	for k := range s.Properties {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

func requiredSet(s *schema) map[string]bool {
	m := map[string]bool{}
	for _, r := range s.Required {
		m[r] = true
	}
	return m
}

func wrap(text string, prefix string, width int) string {
	if strings.TrimSpace(text) == "" {
		return ""
	}
	words := strings.Fields(text)
	var b strings.Builder
	line := prefix
	started := false
	for _, w := range words {
		if started && len(line)+1+len(w) > width {
			b.WriteString(line + "\n")
			line = prefix
			started = false
		}
		if started {
			line += " "
		}
		line += w
		started = true
	}
	if started {
		b.WriteString(line + "\n")
	}
	return b.String()
}

// ---------- Go ----------

var initialisms = map[string]string{
	"id": "ID", "cwd": "CWD", "pid": "PID", "tty": "TTY", "ok": "OK",
	"url": "URL", "api": "API", "http": "HTTP", "json": "JSON",
}

func goName(prop string) string {
	parts := strings.Split(prop, "_")
	var b strings.Builder
	for _, p := range parts {
		if p == "" {
			continue
		}
		if up, ok := initialisms[strings.ToLower(p)]; ok {
			b.WriteString(up)
			continue
		}
		seg := strings.ToUpper(p[:1]) + p[1:]
		// A camelCase property such as sessionId carries its own boundary; only
		// a trailing initialism is rewritten, so Idle never becomes IDle.
		if strings.HasSuffix(seg, "Id") && len(seg) > 2 {
			seg = seg[:len(seg)-2] + "ID"
		}
		b.WriteString(seg)
	}
	return b.String()
}

func goType(s *schema, optional bool) string {
	if d := target(s); d != nil {
		if isEnum(d.s) {
			return d.name
		}
		if optional || s.Nullable {
			return "*" + d.name
		}
		return d.name
	}
	switch s.Type {
	case "string":
		return "string"
	case "integer":
		return "int64"
	case "number":
		return "float64"
	case "boolean":
		return "bool"
	case "array":
		if s.Items == nil {
			fatal("array without items")
		}
		return "[]" + goType(s.Items, false)
	case "object":
		fatal("inline object: give it a name under $defs")
	}
	fatal("unsupported type %q", s.Type)
	return ""
}

func renderGo(names []string) []byte {
	var b strings.Builder
	b.WriteString("// Code generated by tools/contract-gen from api/v1. DO NOT EDIT.\n")
	b.WriteString("//\n")
	b.WriteString("// The schemas are the contract. Edit those and rerun:\n")
	b.WriteString("//\n")
	b.WriteString("//\tgo run ./tools/contract-gen\n")
	b.WriteString("\n// Package contract carries the wire types this daemon speaks.\n")
	b.WriteString("//\n")
	b.WriteString("// They are deliberately separate from the domain types. A transport that\n")
	b.WriteString("// marshalled its domain directly would let an internal rename change the API\n")
	b.WriteString("// with nothing to notice, which is the defect this package exists to remove.\n")
	b.WriteString("package contract\n")

	for _, n := range names {
		d := defs[n]
		b.WriteString("\n")
		b.WriteString(wrap(d.s.Description, "// ", 80))
		if isEnum(d.s) {
			fmt.Fprintf(&b, "type %s string\n\nconst (\n", n)
			for _, v := range d.s.Enum {
				fmt.Fprintf(&b, "\t%s%s %s = %q\n", n, goName(v), n, v)
			}
			b.WriteString(")\n")
			// A generated list of every legal value, so a switch can be checked
			// against the contract instead of against someone's memory of it.
			fmt.Fprintf(&b, "\n// %sValues is every value the contract allows, in contract order.\nvar %sValues = []%s{", n, n, n)
			for i, v := range d.s.Enum {
				if i > 0 {
					b.WriteString(", ")
				}
				fmt.Fprintf(&b, "%s%s", n, goName(v))
			}
			b.WriteString("}\n")
			continue
		}
		if d.s.Type != "object" {
			fatal("%s: top-level definitions must be objects or string enums", n)
		}
		req := requiredSet(d.s)
		fmt.Fprintf(&b, "type %s struct {\n", n)
		props := sortedProps(d.s)
		for i, p := range props {
			ps := d.s.Properties[p]
			if doc := wrap(ps.Description, "\t// ", 84); doc != "" {
				if i > 0 {
					b.WriteString("\n")
				}
				b.WriteString(doc)
			}
			tag := p
			// A nullable field keeps its key and carries null; only an optional
			// one may vanish.
			if !req[p] && !ps.Nullable {
				tag += ",omitempty"
			}
			fmt.Fprintf(&b, "\t%s %s `json:%q`\n", goName(p), goType(ps, !req[p]), tag)
		}
		b.WriteString("}\n")
	}
	// gofmt is run here rather than left to a separate step, so the file on disk
	// is the file `-check` compares against no matter who ran what.
	out, err := format.Source([]byte(b.String()))
	if err != nil {
		fatal("generated Go does not parse: %v", err)
	}
	return out
}

// ---------- TypeScript ----------

func tsType(s *schema) string {
	if d := target(s); d != nil {
		return d.name
	}
	switch s.Type {
	case "string":
		return "string"
	case "integer", "number":
		return "number"
	case "boolean":
		return "boolean"
	case "array":
		if s.Items == nil {
			fatal("array without items")
		}
		inner := tsType(s.Items)
		if strings.ContainsAny(inner, "|") {
			return "(" + inner + ")[]"
		}
		return inner + "[]"
	case "object":
		fatal("inline object: give it a name under $defs")
	}
	fatal("unsupported type %q", s.Type)
	return ""
}

func renderTS(names []string) []byte {
	var b strings.Builder
	b.WriteString("// Code generated by tools/contract-gen from api/v1. DO NOT EDIT.\n")
	b.WriteString("//\n")
	b.WriteString("// The schemas are the contract. Edit those and rerun:\n")
	b.WriteString("//\n")
	b.WriteString("//   go run ./tools/contract-gen\n")
	b.WriteString("//\n")
	b.WriteString("// This module holds types only. It imports nothing, touches no DOM and no\n")
	b.WriteString("// React Native API, so the web console and a native app can both read it.\n")

	for _, n := range names {
		d := defs[n]
		b.WriteString("\n")
		b.WriteString(jsdoc(d.s.Description, ""))
		if isEnum(d.s) {
			fmt.Fprintf(&b, "export type %s =\n", n)
			for i, v := range d.s.Enum {
				sep := "|"
				if i == 0 {
					sep = " "
				}
				fmt.Fprintf(&b, "  %s %q\n", sep, v)
			}
			fmt.Fprintf(&b, "\nexport const %sValues: readonly %s[] = [", n, n)
			for i, v := range d.s.Enum {
				if i > 0 {
					b.WriteString(", ")
				}
				fmt.Fprintf(&b, "%q", v)
			}
			b.WriteString("] as const\n")
			continue
		}
		req := requiredSet(d.s)
		fmt.Fprintf(&b, "export interface %s {\n", n)
		props := sortedProps(d.s)
		for i, p := range props {
			ps := d.s.Properties[p]
			if doc := jsdoc(ps.Description, "  "); doc != "" {
				if i > 0 {
					b.WriteString("\n")
				}
				b.WriteString(doc)
			}
			opt := ""
			if !req[p] && !ps.Nullable {
				opt = "?"
			}
			t := tsType(ps)
			if ps.Nullable {
				t += " | null"
			}
			fmt.Fprintf(&b, "  %s%s: %s\n", tsProp(p), opt, t)
		}
		b.WriteString("}\n")
	}
	return []byte(b.String())
}

// tsProp quotes a property only when it is not a plain identifier, so the
// generated file reads like something a person would have written.
func tsProp(p string) string {
	for i, r := range p {
		ok := r == '_' || (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z')
		if i > 0 {
			ok = ok || (r >= '0' && r <= '9')
		}
		if !ok {
			return fmt.Sprintf("%q", p)
		}
	}
	return p
}

func jsdoc(text, indent string) string {
	if strings.TrimSpace(text) == "" {
		return ""
	}
	body := wrap(text, indent+" * ", 84)
	return indent + "/**\n" + body + indent + " */\n"
}
