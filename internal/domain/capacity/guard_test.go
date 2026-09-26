package capacity

import (
	"bufio"
	"go/ast"
	"go/parser"
	"go/token"
	"go/types"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"testing"
)

// The register's two guards (docs/limits.md §4.1).
//
// The first walks the register: every row answers the four questions, its
// at-limit behaviour is one its class allows or it says why not, and its limit
// can be lowered. The second reads this repository's source: every declaration
// that bounds something — a constant or variable named max…, Maximum… or
// …Limit, or a buffered channel — is either a registered row's source or on
// the baseline, testdata/baseline.txt, with the limits.md row that will
// register it. Adding a bounded thing without doing one of the two is a
// failing test; so is leaving a baseline line behind after the thing it names
// has gone.

// boundName is the naming convention the scan relies on. It is a convention
// and not a proof: a bound named anything else passes unseen, which is written
// down in the C1 report rather than pretended away.
var boundName = regexp.MustCompile(`^[mM]ax(?:imum)?(?:[A-Z0-9_]|$)|Limit$|^limit$`)

// scan answers every bounded declaration under root/dirs, keyed as the
// register's Sources and the baseline spell them:
//
//	internal/adapters/board.MaximumStoreBytes           a package-level name
//	internal/app.(*Screens).watch.maxWait               a constant inside a function
//	internal/transport/cloud.(*Relay).start:chan(r.depth())   a buffered channel
func scan(t *testing.T, root string, dirs ...string) map[string]string {
	t.Helper()
	found := map[string]string{}
	for _, dir := range dirs {
		err := filepath.WalkDir(filepath.Join(root, dir), func(path string, d os.DirEntry, err error) error {
			if err != nil {
				return err
			}
			name := d.Name()
			if d.IsDir() {
				if name == "testdata" || name == "node_modules" || name == "vendor" || strings.HasPrefix(name, ".") {
					return filepath.SkipDir
				}
				return nil
			}
			if !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
				return nil
			}
			rel, err := filepath.Rel(root, filepath.Dir(path))
			if err != nil {
				return err
			}
			scanFile(t, path, filepath.ToSlash(rel), found)
			return nil
		})
		if err != nil {
			t.Fatalf("scanning %s: %v", dir, err)
		}
	}
	return found
}

func scanFile(t *testing.T, path, pkg string, found map[string]string) {
	t.Helper()
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, path, nil, parser.SkipObjectResolution)
	if err != nil {
		t.Fatalf("parsing %s: %v", path, err)
	}
	add := func(key string, at token.Pos) {
		if _, dup := found[key]; dup {
			// Two identical declarations under one name are one line on the
			// baseline; the second is told apart by position.
			key += "#" + fset.Position(at).String()
		}
		found[key] = fset.Position(at).String()
	}
	names := func(decl *ast.GenDecl, prefix string, tok token.Token) {
		if decl.Tok != tok && !(tok == token.VAR && decl.Tok == token.CONST) {
			return
		}
		for _, spec := range decl.Specs {
			vs, ok := spec.(*ast.ValueSpec)
			if !ok {
				continue
			}
			for _, id := range vs.Names {
				if boundName.MatchString(id.Name) {
					add(pkg+"."+prefix+id.Name, id.Pos())
				}
			}
		}
	}
	chans := func(node ast.Node, owner string) {
		ast.Inspect(node, func(n ast.Node) bool {
			call, ok := n.(*ast.CallExpr)
			if !ok || len(call.Args) != 2 {
				return true
			}
			if fn, ok := call.Fun.(*ast.Ident); !ok || fn.Name != "make" {
				return true
			}
			if _, ok := call.Args[0].(*ast.ChanType); !ok {
				return true
			}
			// A channel of one is a latch or a signal, not a queue.
			if lit, ok := call.Args[1].(*ast.BasicLit); ok && (lit.Value == "0" || lit.Value == "1") {
				return true
			}
			add(pkg+"."+owner+":chan("+types.ExprString(call.Args[1])+")", call.Pos())
			return true
		})
	}
	for _, decl := range file.Decls {
		switch d := decl.(type) {
		case *ast.GenDecl:
			// Package-level: constants and variables both.
			names(d, "", token.VAR)
			chans(d, "package")
		case *ast.FuncDecl:
			owner := funcName(d)
			if d.Body == nil {
				continue
			}
			ast.Inspect(d.Body, func(n ast.Node) bool {
				if g, ok := n.(*ast.GenDecl); ok {
					// Inside a function only constants: a local variable
					// called limit is a parameter of one call, not a bound
					// something accumulates against.
					names(g, owner+".", token.CONST)
				}
				return true
			})
			chans(d.Body, owner)
		}
	}
}

func funcName(d *ast.FuncDecl) string {
	if d.Recv == nil || len(d.Recv.List) == 0 {
		return d.Name.Name
	}
	recv := d.Recv.List[0].Type
	star := false
	if s, ok := recv.(*ast.StarExpr); ok {
		star, recv = true, s.X
	}
	switch r := recv.(type) {
	case *ast.IndexExpr:
		recv = r.X
	case *ast.IndexListExpr:
		recv = r.X
	}
	name := types.ExprString(recv)
	if star {
		return "(*" + name + ")." + d.Name.Name
	}
	return name + "." + d.Name.Name
}

func moduleRoot(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatal("no go.mod above the test's directory")
		}
		dir = parent
	}
}

// readBaseline is testdata/baseline.txt: `key # why it is not registered yet`,
// one per line. A line without a reason is refused: an unexplained exemption is
// a hole with a name on it.
func readBaseline(t *testing.T, path string) map[string]string {
	t.Helper()
	f, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	out := map[string]string{}
	sc := bufio.NewScanner(f)
	for n := 1; sc.Scan(); n++ {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		key, why, ok := strings.Cut(line, " # ")
		key, why = strings.TrimSpace(key), strings.TrimSpace(why)
		if !ok || key == "" || why == "" {
			t.Errorf("baseline line %d has no reason: %q", n, line)
			continue
		}
		if _, dup := out[key]; dup {
			t.Errorf("baseline line %d repeats %s", n, key)
		}
		out[key] = why
	}
	if err := sc.Err(); err != nil {
		t.Fatal(err)
	}
	return out
}

func TestRegisterRowsAnswerTheFourQuestions(t *testing.T) {
	names := map[string]bool{}
	deviating := []string{}
	for _, e := range Register() {
		if !namePattern.MatchString(e.Name) || names[e.Name] {
			t.Errorf("%q: a row needs a unique area.thing name", e.Name)
		}
		names[e.Name] = true
		if !KnownClass(e.Class) {
			t.Errorf("%s: class %q is not a class", e.Name, e.Class)
		}
		if e.Unit != Bytes && e.Unit != Characters && e.Unit != Rows && e.Unit != Seconds {
			t.Errorf("%s: unit %q", e.Name, e.Unit)
		}
		// The limit.
		if e.Limit <= 0 {
			t.Errorf("%s: no limit", e.Name)
		}
		if w := e.Warn(); w <= 0 || w >= criticalAt {
			t.Errorf("%s: warn_at %v is not below critical", e.Name, w)
		}
		// What happens when it is full.
		if e.AtLimit == "" {
			t.Errorf("%s: nothing says what happens at the limit", e.Name)
		}
		if !Allowed(e.Class, e.AtLimit) {
			if e.Deviation == "" {
				t.Errorf("%s: %s is not allowed for %s and the row does not say why", e.Name, e.AtLimit, e.Class)
			}
			deviating = append(deviating, e.Name)
		} else if e.Deviation != "" {
			t.Errorf("%s: a deviation on a row whose behaviour its class allows", e.Name)
		}
		// Who finds out.
		told := map[Channel]bool{}
		for _, c := range e.Told {
			told[c] = true
		}
		if !told[Diagnostics] {
			t.Errorf("%s: every row is in diagnostics", e.Name)
		}
		if (e.Class == Evidence || e.Class == SecurityAudit) != told[Health] {
			t.Errorf("%s: health is told exactly for evidence and security-audit rows", e.Name)
		}
		// Who decides eviction.
		if e.EvictedBy != Person && e.EvictedBy != Daemon {
			t.Errorf("%s: nobody decides eviction", e.Name)
		}
		if (e.Class == Evidence || e.Class == SecurityAudit) && e.EvictedBy != Person {
			t.Errorf("%s: only a person may let go of %s", e.Name, e.Class)
		}
		// Injectable: lowered by an override, and never raised. A limit of
		// one is "any at all" and has nothing under it to lower to.
		if e.Limit > 1 {
			lower, problems := Resolve([]Entry{e}, e.Name+"="+itoa(e.Limit-1))
			if len(problems) != 0 || lower[0].Limit != e.Limit-1 || !lower[0].Overridden {
				t.Errorf("%s: the limit cannot be lowered: %v %v", e.Name, lower, problems)
			}
		}
		higher, problems := Resolve([]Entry{e}, e.Name+"="+itoa(e.Limit+1))
		if len(problems) != 1 || higher[0].Limit != e.Limit || higher[0].Overridden {
			t.Errorf("%s: an override raised the limit: %v %v", e.Name, higher, problems)
		}
	}
	// The rows that do not yet do what their class requires, by name. A new
	// one is a decision to write down here, not a string to add to a row.
	sort.Strings(deviating)
	if want := []string{StoreDB}; strings.Join(deviating, ",") != strings.Join(want, ",") {
		t.Errorf("rows deviating from their class: %v, want %v", deviating, want)
	}
}

func TestEveryBoundIsRegistered(t *testing.T) {
	root := moduleRoot(t)
	found := scan(t, root, "internal", "cmd")
	if len(found) < 50 {
		// The scan finding almost nothing is the scan broken, not the
		// repository clean (DG-8).
		t.Fatalf("the scan found %d bounded declarations; it is not reading the tree", len(found))
	}
	registered := map[string]string{}
	for _, e := range Register() {
		for _, s := range e.Sources {
			registered[s] = e.Name
		}
	}
	baseline := readBaseline(t, filepath.Join(root, "internal", "domain", "capacity", "testdata", "baseline.txt"))
	var unregistered, stale []string
	for key, at := range found {
		if registered[key] == "" && baseline[key] == "" {
			unregistered = append(unregistered, key+"   ("+at+")")
		}
	}
	for key := range baseline {
		if _, ok := found[key]; !ok {
			stale = append(stale, key)
		}
	}
	for key, row := range registered {
		if _, ok := found[key]; !ok {
			t.Errorf("%s names a source that is not in the tree: %s", row, key)
		}
		if baseline[key] != "" {
			t.Errorf("%s is registered to %s and on the baseline", key, row)
		}
	}
	sort.Strings(unregistered)
	sort.Strings(stale)
	for _, u := range unregistered {
		t.Errorf("a bounded declaration that is not registered: %s — register it in capacity.Register, or put it on testdata/baseline.txt with the limits.md row that will", u)
	}
	for _, s := range stale {
		t.Errorf("the baseline names something that is no longer in the tree: %s — take its line out", s)
	}
}

// The second guard, shown to go red: a file declaring a bounded constant and a
// buffered channel, scanned against an empty baseline, is two findings.
func TestTheScanFindsAnUnregisteredBound(t *testing.T) {
	root := moduleRoot(t)
	found := scan(t, filepath.Join(root, "internal", "domain", "capacity", "testdata"), "redsample")
	for _, want := range []string{
		"redsample.MaximumWidgets",
		"redsample.widgetLimit",
		"redsample.(*Pump).start:chan(depth)",
		"redsample.(*Pump).start.maxRetries",
	} {
		if _, ok := found[want]; !ok {
			t.Errorf("the scan did not find %s; found %v", want, found)
		}
	}
	for _, not := range []string{"redsample.maximal", "redsample.(*Pump).start:chan(1)", "redsample.(*Pump).start.limit"} {
		if _, ok := found[not]; ok {
			t.Errorf("the scan took %s for a bound", not)
		}
	}
}

func itoa(n int64) string { return strconv.FormatInt(n, 10) }
