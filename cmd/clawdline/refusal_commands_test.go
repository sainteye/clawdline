package main

import (
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"testing"
)

// TestRefusalCommandsRun holds every `clawdline …` command a message in the
// daemon or the CLI quotes to the CLI it names: the subcommand exists, each
// --flag is one that subcommand's usage lists, and each flag its usage
// requires (outside any [ ] or ( ) group) is there. Messages are read from the
// source, with every non-literal part standing in as a placeholder, and the
// legal flags from the usage functions themselves, so neither side is a
// hand-copied list. A refusal that hands a caller a command that exits 2 is
// worse than one that names none.
func TestRefusalCommandsRun(t *testing.T) {
	commands := map[string]bool{}
	for _, c := range topLevelCommands(t) {
		commands[c] = true
	}
	specs := usageSpecs(t)
	quoted := quotedCommands(t)
	if len(quoted) == 0 {
		t.Fatal("no quoted clawdline command was found in the source; the scan is broken")
	}
	for _, q := range quoted {
		args := shellWords(q.text)
		if len(args) < 2 || args[0] != "clawdline" {
			continue
		}
		if !commands[args[1]] {
			t.Errorf("%s: `%s`: clawdline has no subcommand %q", q.at, q.text, args[1])
			continue
		}
		key := args[1]
		if len(args) > 2 {
			if _, ok := specs[key+" "+args[2]]; ok {
				key += " " + args[2]
			}
		}
		spec, ok := specs[key]
		if !ok {
			// A subcommand with no usage synopsis (`open`) has no flag list
			// to hold a command to; that it exists is what is checked.
			continue
		}
		given := map[string]bool{}
		for _, a := range args[2:] {
			if !strings.HasPrefix(a, "--") || !usageFlag.MatchString(a) {
				continue
			}
			name := usageFlag.FindStringSubmatch(a)[1]
			given[name] = true
			if !spec.legal[name] {
				t.Errorf("%s: `%s`: --%s is not a flag of clawdline %s", q.at, q.text, name, key)
			}
		}
		for _, name := range spec.required {
			if !given[name] {
				t.Errorf("%s: `%s`: clawdline %s requires --%s", q.at, q.text, key, name)
			}
		}
	}
}

type quotedCommand struct {
	at, text string
}

var quotedCommandPattern = regexp.MustCompile("`(clawdline [^`]*)`")

// quotedCommands is every backquoted `clawdline …` span in a string the
// daemon or the CLI builds, outside tests and generated code.
func quotedCommands(t *testing.T) []quotedCommand {
	t.Helper()
	var out []quotedCommand
	fset := token.NewFileSet()
	for _, root := range []string{"../../internal", "."} {
		err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			name := d.Name()
			if d.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") ||
				strings.HasPrefix(name, "zz_") {
				return nil
			}
			file, err := parser.ParseFile(fset, path, nil, 0)
			if err != nil {
				return err
			}
			ast.Inspect(file, func(n ast.Node) bool {
				var text string
				switch n := n.(type) {
				case *ast.BinaryExpr:
					if n.Op != token.ADD {
						return true
					}
					text = renderString(n)
				case *ast.BasicLit:
					text = renderString(n)
				case *ast.CallExpr:
					if !isFormatCall(n) {
						return true
					}
					text = renderString(n)
				default:
					return true
				}
				for _, m := range quotedCommandPattern.FindAllStringSubmatch(text, -1) {
					out = append(out, quotedCommand{at: fset.Position(n.Pos()).String(), text: m[1]})
				}
				_, isCall := n.(*ast.CallExpr)
				return isCall
			})
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
	}
	return dedupe(out)
}

func dedupe(in []quotedCommand) []quotedCommand {
	seen := map[string]bool{}
	var out []quotedCommand
	for _, q := range in {
		if k := q.at + "\x00" + q.text; !seen[k] {
			seen[k] = true
			out = append(out, q)
		}
	}
	return out
}

func isFormatCall(c *ast.CallExpr) bool {
	sel, ok := c.Fun.(*ast.SelectorExpr)
	if !ok || len(c.Args) == 0 {
		return false
	}
	switch sel.Sel.Name {
	case "Sprintf", "Errorf":
		return true
	}
	return false
}

var formatVerb = regexp.MustCompile(`%[-+# 0-9.]*[a-zA-Z]`)

// renderString is the text an expression builds, with every part that is
// not a string literal written as <v>.
func renderString(e ast.Expr) string {
	switch e := e.(type) {
	case *ast.BasicLit:
		if e.Kind != token.STRING {
			return "<v>"
		}
		s, err := strconv.Unquote(e.Value)
		if err != nil {
			return "<v>"
		}
		return s
	case *ast.BinaryExpr:
		if e.Op == token.ADD {
			return renderString(e.X) + renderString(e.Y)
		}
	case *ast.ParenExpr:
		return renderString(e.X)
	case *ast.CallExpr:
		if isFormatCall(e) {
			format := renderString(e.Args[0])
			if sel := e.Fun.(*ast.SelectorExpr); sel.Sel.Name == "Fprintf" {
				return "<v>"
			}
			return formatVerb.ReplaceAllString(format, "<v>")
		}
	}
	return "<v>"
}

// shellWords splits a command as a shell would, for the quoting the
// messages use: double quotes group, and nothing else is special.
func shellWords(s string) []string {
	var out []string
	var cur strings.Builder
	in, started := false, false
	for _, r := range s {
		switch {
		case r == '"':
			in, started = !in, true
		case (r == ' ' || r == '\t') && !in:
			if started {
				out = append(out, cur.String())
				cur.Reset()
				started = false
			}
		default:
			cur.WriteRune(r)
			started = true
		}
	}
	if started {
		out = append(out, cur.String())
	}
	return out
}

// topLevelCommands is every case of main's dispatch on os.Args[1].
func topLevelCommands(t *testing.T) []string {
	t.Helper()
	file, err := parser.ParseFile(token.NewFileSet(), "main.go", nil, 0)
	if err != nil {
		t.Fatal(err)
	}
	var out []string
	for _, decl := range file.Decls {
		fn, ok := decl.(*ast.FuncDecl)
		if !ok || fn.Name.Name != "main" {
			continue
		}
		ast.Inspect(fn, func(n ast.Node) bool {
			sw, ok := n.(*ast.SwitchStmt)
			if !ok {
				return true
			}
			idx, ok := sw.Tag.(*ast.IndexExpr)
			if !ok {
				return true
			}
			if lit, ok := idx.Index.(*ast.BasicLit); !ok || lit.Value != "1" {
				return true
			}
			for _, stmt := range sw.Body.List {
				for _, e := range stmt.(*ast.CaseClause).List {
					if lit, ok := e.(*ast.BasicLit); ok {
						s, _ := strconv.Unquote(lit.Value)
						out = append(out, s)
					}
				}
			}
			return false
		})
	}
	if len(out) == 0 {
		t.Fatal("main's subcommand switch was not found")
	}
	return out
}

type usageSpec struct {
	legal    map[string]bool
	required []string
}

var usageFlag = regexp.MustCompile(`--([a-z][a-z0-9-]*)`)

// usageSpecs reads every usage function in this package: a line that starts
// `clawdline <cmd> [<sub>]` opens a synopsis, and the deeper-indented lines
// that follow continue it. A flag outside every [ ] and ( ) group is
// required.
func usageSpecs(t *testing.T) map[string]usageSpec {
	t.Helper()
	fset := token.NewFileSet()
	pkgs, err := parser.ParseDir(fset, ".", func(fi fs.FileInfo) bool {
		return !strings.HasSuffix(fi.Name(), "_test.go")
	}, 0)
	if err != nil {
		t.Fatal(err)
	}
	synopses := map[string][]string{}
	for _, pkg := range pkgs {
		for _, file := range pkg.Files {
			for _, decl := range file.Decls {
				fn, ok := decl.(*ast.FuncDecl)
				if !ok {
					continue
				}
				usageOnly := !strings.HasSuffix(strings.ToLower(fn.Name.Name), "usage")
				var lines []string
				ast.Inspect(fn, func(n ast.Node) bool {
					c, ok := n.(*ast.CallExpr)
					if !ok {
						return true
					}
					sel, ok := c.Fun.(*ast.SelectorExpr)
					if !ok || (sel.Sel.Name != "Fprintln" && sel.Sel.Name != "Fprintf") || len(c.Args) < 2 {
						return true
					}
					lines = append(lines, renderString(c.Args[1]))
					return false
				})
				collectSynopses(lines, usageOnly, synopses)
			}
		}
	}
	specs := map[string]usageSpec{}
	for key, texts := range synopses {
		spec := usageSpec{legal: map[string]bool{}}
		// A command with several synopses (`handoff --summary f` and
		// `handoff --summary f --check`) requires only what every one does.
		counts := map[string]int{}
		for _, text := range texts {
			for _, name := range synopsisFlags(text, spec.legal) {
				counts[name]++
			}
		}
		for name, n := range counts {
			if n == len(texts) {
				spec.required = append(spec.required, name)
			}
		}
		sort.Strings(spec.required)
		specs[key] = spec
	}
	return specs
}

// synopsisFlags adds every flag of one synopsis to legal and answers those
// outside every [ ] and ( ) group.
func synopsisFlags(text string, legal map[string]bool) []string {
	var required []string
	{
		depth := 0
		for i := 0; i < len(text); i++ {
			switch text[i] {
			case '[', '(':
				depth++
			case ']', ')':
				depth--
			}
			if m := usageFlag.FindStringSubmatch(text[i:]); m != nil && strings.HasPrefix(text[i:], m[0]) &&
				(i == 0 || text[i-1] == ' ' || text[i-1] == '[' || text[i-1] == '(' || text[i-1] == '|') {
				legal[m[1]] = true
				if depth == 0 {
					required = append(required, m[1])
				}
				i += len(m[0]) - 1
			}
		}
	}
	return required
}

var synopsisStart = regexp.MustCompile(`^\s*(?:usage:\s+)?clawdline ([a-z][a-z0-9-]*)(?: ([a-z][a-z0-9-]*))?(.*)$`)

func collectSynopses(lines []string, usageOnly bool, into map[string][]string) {
	key, indent := "", 0
	for _, line := range lines {
		if m := synopsisStart.FindStringSubmatch(line); m != nil &&
			(!usageOnly || strings.HasPrefix(strings.TrimSpace(line), "usage:")) {
			key = m[1]
			rest := m[3]
			if m[2] != "" {
				key += " " + m[2]
			} else if m[2] == "" && strings.HasPrefix(rest, " ") {
				rest = strings.TrimPrefix(rest, " ")
			}
			indent = len(line) - len(strings.TrimLeft(line, " "))
			into[key] = append(into[key], rest)
			continue
		}
		lead := len(line) - len(strings.TrimLeft(line, " "))
		if key != "" && lead > indent+4 {
			into[key][len(into[key])-1] += " " + strings.TrimSpace(line)
			continue
		}
		key = ""
	}
}
