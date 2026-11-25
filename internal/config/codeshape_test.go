package config_test

import (
	"bufio"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"testing"
)

// Two ratchets on the shape of the Go code, so that work done one small
// increment at a time cannot quietly pile every new case into the nearest
// function or reach across a layer because it was the shortest path.
//
// Layers: docs/plan.md §3 says `domain` imports no adapter or transport, and
// `transport` imports no adapter; docs/architecture.md adds that app use cases
// reach the platform through ports. The second rule and the app rule were not
// true when this guard was written, so the edges that broke them are listed in
// testdata/layer-baseline.txt. A new edge fails. A listed edge that is gone
// fails too, so the list only shrinks. Nothing under `domain` may be listed.
//
// Functions: a function with more than maxFunctionStatements statements or
// maxFunctionBranches branches is hard to review because its correctness
// depends on conditions established far from the line being read. Those that
// already existed are in testdata/function-size-baseline.txt with their size.
// They may not grow; a new one over either limit fails; one that has shrunk or
// gone must have its line lowered or removed. Statements, not lines, are
// counted, so a long data table (one return of a literal) is not a long
// function.
//
// Only production files are measured: tests may wire real adapters together.
// Generated files are skipped. Every GOOS file is read, not only this
// machine's, so a Windows-only file cannot hide an edge.
//
// After a refactor shrinks something, run the guard with
// CLAWDLINE_TIGHTEN_SHAPE_BASELINES=1 to rewrite both lists. That mode only
// removes entries and lowers numbers; it never adds or raises one, so growing
// past the baseline is a hand edit that a reviewer sees in the diff.

const (
	maxFunctionStatements = 100
	maxFunctionBranches   = 40
)

const tightenShapeBaselines = "CLAWDLINE_TIGHTEN_SHAPE_BASELINES"

// forbiddenLayers maps a layer to the layers its production code may not
// import.
var forbiddenLayers = map[string][]string{
	"domain":    {"app", "adapters", "transport", "cmd"},
	"app":       {"adapters", "transport", "cmd"},
	"adapters":  {"transport", "cmd"},
	"transport": {"adapters", "cmd"},
}

// layerOf names the layer of a repository-relative package directory or
// module-relative import path, or "" outside the layered tree.
func layerOf(rel string) string {
	parts := strings.Split(filepath.ToSlash(rel), "/")
	if parts[0] == "cmd" {
		return "cmd"
	}
	if len(parts) >= 2 && parts[0] == "internal" {
		switch parts[1] {
		case "domain", "app", "adapters", "transport":
			return parts[1]
		}
	}
	return ""
}

func forbidden(fromLayer, toLayer string) bool {
	for _, l := range forbiddenLayers[fromLayer] {
		if l == toLayer {
			return true
		}
	}
	return false
}

type functionSize struct {
	statements, branches int
}

func (s functionSize) over() bool {
	return s.statements > maxFunctionStatements || s.branches > maxFunctionBranches
}

// measureFunction counts the statements and branches in a body, including
// those of the closures it declares: a closure is still read as part of the
// function that holds it.
func measureFunction(body *ast.BlockStmt) functionSize {
	var s functionSize
	ast.Inspect(body, func(n ast.Node) bool {
		switch n.(type) {
		case *ast.BlockStmt, *ast.EmptyStmt:
		case ast.Stmt:
			s.statements++
		}
		switch n.(type) {
		case *ast.IfStmt, *ast.CaseClause, *ast.CommClause, *ast.ForStmt, *ast.RangeStmt:
			s.branches++
		}
		return true
	})
	return s
}

func functionName(fd *ast.FuncDecl) string {
	if fd.Recv == nil || len(fd.Recv.List) == 0 {
		return fd.Name.Name
	}
	t := fd.Recv.List[0].Type
	for {
		switch x := t.(type) {
		case *ast.StarExpr:
			t = x.X
			continue
		case *ast.IndexExpr:
			t = x.X
			continue
		case *ast.IndexListExpr:
			t = x.X
			continue
		}
		break
	}
	if id, ok := t.(*ast.Ident); ok {
		return id.Name + "." + fd.Name.Name
	}
	return fd.Name.Name
}

// measureFile returns every function in one parsed file keyed by
// "path<TAB>name"; repeated names (init) get "#2", "#3".
func measureFile(rel string, f *ast.File, into map[string]functionSize) {
	seen := map[string]int{}
	for _, d := range f.Decls {
		fd, ok := d.(*ast.FuncDecl)
		if !ok || fd.Body == nil {
			continue
		}
		name := functionName(fd)
		seen[name]++
		if seen[name] > 1 {
			name += "#" + strconv.Itoa(seen[name])
		}
		into[rel+"\t"+name] = measureFunction(fd.Body)
	}
}

type codeShape struct {
	edges     map[string]bool // "from to", forbidden-layer edges only
	functions map[string]functionSize
	files     int
}

func scanCodeShape(t *testing.T, root string) codeShape {
	t.Helper()
	module := modulePath(t, root)
	shape := codeShape{edges: map[string]bool{}, functions: map[string]functionSize{}}
	fset := token.NewFileSet()
	for _, top := range []string{"internal", "cmd"} {
		err := filepath.WalkDir(filepath.Join(root, top), func(path string, d os.DirEntry, err error) error {
			if err != nil {
				return err
			}
			name := d.Name()
			if d.IsDir() {
				if name == "testdata" || name == "vendor" || name == "node_modules" || (strings.HasPrefix(name, ".") && path != root) {
					return filepath.SkipDir
				}
				return nil
			}
			if !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
				return nil
			}
			f, err := parser.ParseFile(fset, path, nil, parser.ParseComments|parser.SkipObjectResolution)
			if err != nil {
				return err
			}
			if ast.IsGenerated(f) {
				return nil
			}
			rel, err := filepath.Rel(root, path)
			if err != nil {
				return err
			}
			rel = filepath.ToSlash(rel)
			shape.files++
			pkg := filepath.ToSlash(filepath.Dir(rel))
			from := layerOf(pkg)
			for _, imp := range f.Imports {
				target, err := strconv.Unquote(imp.Path.Value)
				if err != nil || !strings.HasPrefix(target, module+"/") {
					continue
				}
				target = strings.TrimPrefix(target, module+"/")
				if forbidden(from, layerOf(target)) {
					shape.edges[pkg+" "+target] = true
				}
			}
			measureFile(rel, f, shape.functions)
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
	}
	return shape
}

func modulePath(t *testing.T, root string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(root, "go.mod"))
	if err != nil {
		t.Fatal(err)
	}
	for _, line := range strings.Split(string(b), "\n") {
		if rest, ok := strings.CutPrefix(strings.TrimSpace(line), "module "); ok {
			return strings.TrimSpace(rest)
		}
	}
	t.Fatal("go.mod names no module")
	return ""
}

// baselineLines reads a baseline file: blank lines and # comments are skipped,
// and the leading comment block is returned so a rewrite keeps it.
func baselineLines(t *testing.T, path string) (header []string, lines []string) {
	t.Helper()
	f, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	inHeader := true
	for sc.Scan() {
		line := sc.Text()
		if strings.HasPrefix(line, "#") {
			if inHeader {
				header = append(header, line)
			}
			continue
		}
		inHeader = false
		if strings.TrimSpace(line) != "" {
			lines = append(lines, line)
		}
	}
	if err := sc.Err(); err != nil {
		t.Fatal(err)
	}
	return header, lines
}

func writeBaseline(t *testing.T, path string, header, lines []string) {
	t.Helper()
	sort.Strings(lines)
	out := strings.Join(header, "\n") + "\n" + strings.Join(lines, "\n") + "\n"
	if err := os.WriteFile(path, []byte(out), 0o644); err != nil {
		t.Fatal(err)
	}
}

// layerProblems compares the forbidden edges found with the baseline.
// tightened is the baseline with every gone edge removed.
func layerProblems(found map[string]bool, baseline []string) (problems, tightened []string) {
	listed := map[string]bool{}
	for _, e := range baseline {
		if listed[e] {
			problems = append(problems, "listed twice in the layer baseline: "+e)
			continue
		}
		listed[e] = true
		if layerOf(strings.Fields(e)[0]) == "domain" {
			problems = append(problems, "domain may never be on the layer baseline: "+e)
		}
		if found[e] {
			tightened = append(tightened, e)
		} else {
			problems = append(problems, "the layer baseline names an import that is gone, take its line out: "+e)
		}
	}
	var fresh []string
	for e := range found {
		if !listed[e] {
			fresh = append(fresh, e)
		}
	}
	sort.Strings(fresh)
	for _, e := range fresh {
		parts := strings.Fields(e)
		problems = append(problems, fmt.Sprintf("%s imports %s: a %s package may not import %s. Reach it through a port in internal/app/ports, or move the code to the layer that owns it",
			parts[0], parts[1], layerOf(parts[0]), layerOf(parts[1])))
	}
	return problems, tightened
}

// functionProblems compares measured functions with the baseline.
// tightened is the baseline with gone or fitting entries removed and every
// number lowered to its current value.
func functionProblems(found map[string]functionSize, baseline []string) (problems, tightened []string) {
	listed := map[string]bool{}
	for _, line := range baseline {
		f := strings.Split(line, "\t")
		if len(f) != 4 {
			problems = append(problems, "a function baseline line is not path<TAB>name<TAB>statements<TAB>branches: "+line)
			continue
		}
		st, err1 := strconv.Atoi(f[2])
		br, err2 := strconv.Atoi(f[3])
		if err1 != nil || err2 != nil {
			problems = append(problems, "a function baseline line has a non-number: "+line)
			continue
		}
		key := f[0] + "\t" + f[1]
		where := f[0] + " " + f[1]
		if listed[key] {
			problems = append(problems, "listed twice in the function baseline: "+where)
			continue
		}
		listed[key] = true
		now, ok := found[key]
		switch {
		case !ok:
			problems = append(problems, "the function baseline names a function that is gone, take its line out: "+where)
			continue
		case !now.over():
			problems = append(problems, fmt.Sprintf("%s now fits the limits (%d statements, %d branches), take its line out", where, now.statements, now.branches))
			continue
		}
		if now.statements > st || now.branches > br {
			problems = append(problems, fmt.Sprintf("%s grew to %d statements and %d branches; its baseline is %d and %d. Split out the new case instead of adding it here",
				where, now.statements, now.branches, st, br))
		} else if now.statements < st || now.branches < br {
			problems = append(problems, fmt.Sprintf("%s shrank to %d statements and %d branches; lower its baseline from %d and %d",
				where, now.statements, now.branches, st, br))
		}
		tightened = append(tightened, fmt.Sprintf("%s\t%s\t%d\t%d", f[0], f[1], min(st, now.statements), min(br, now.branches)))
	}
	var fresh []string
	for key, s := range found {
		if s.over() && !listed[key] {
			fresh = append(fresh, fmt.Sprintf("%s has %d statements and %d branches; the limits are %d and %d. Split it into functions that can each be read on their own",
				strings.Replace(key, "\t", " ", 1), s.statements, s.branches, maxFunctionStatements, maxFunctionBranches))
		}
	}
	sort.Strings(fresh)
	return append(problems, fresh...), tightened
}

func TestLayersImportOnlyDownward(t *testing.T) {
	root := repoRoot(t)
	shape := scanCodeShape(t, root)
	if shape.files < 500 {
		t.Fatalf("the scan read %d production Go files; it is not reading the tree", shape.files)
	}
	path := filepath.Join(root, "internal", "config", "testdata", "layer-baseline.txt")
	header, baseline := baselineLines(t, path)
	problems, tightened := layerProblems(shape.edges, baseline)
	if os.Getenv(tightenShapeBaselines) == "1" {
		writeBaseline(t, path, header, tightened)
		t.Logf("rewrote %s with %d edges", path, len(tightened))
		return
	}
	for _, p := range problems {
		t.Error(p)
	}
}

func TestFunctionsDoNotGrowPastTheirSize(t *testing.T) {
	root := repoRoot(t)
	shape := scanCodeShape(t, root)
	if len(shape.functions) < 5000 {
		t.Fatalf("the scan measured %d functions; it is not reading the tree", len(shape.functions))
	}
	path := filepath.Join(root, "internal", "config", "testdata", "function-size-baseline.txt")
	header, baseline := baselineLines(t, path)
	problems, tightened := functionProblems(shape.functions, baseline)
	if os.Getenv(tightenShapeBaselines) == "1" {
		writeBaseline(t, path, header, tightened)
		t.Logf("rewrote %s with %d functions", path, len(tightened))
		return
	}
	for _, p := range problems {
		t.Error(p)
	}
}

// The guards shown to go red on the cases they exist for.

func TestALayerGuardRefusesANewEdgeAndAGoneOne(t *testing.T) {
	if !forbidden("domain", layerOf("internal/adapters/store")) || forbidden("adapters", layerOf("internal/app/ports")) {
		t.Fatal("layer rules are not what the docs say")
	}
	found := map[string]bool{
		"internal/domain/work internal/adapters/store":    true,
		"internal/transport/http internal/adapters/store": true,
	}
	problems, tightened := layerProblems(found, []string{
		"internal/transport/http internal/adapters/store",
		"internal/app/orchestrator internal/adapters/store",
	})
	want := []string{
		"the layer baseline names an import that is gone",
		"internal/domain/work imports internal/adapters/store",
	}
	assertProblems(t, problems, want)
	if len(tightened) != 1 || tightened[0] != "internal/transport/http internal/adapters/store" {
		t.Errorf("tightened = %v", tightened)
	}
	problems, _ = layerProblems(found, []string{"internal/domain/work internal/adapters/store", "internal/transport/http internal/adapters/store"})
	assertProblems(t, problems, []string{"domain may never be on the layer baseline"})
}

func TestAFunctionGuardRefusesGrowthAndANewLongFunction(t *testing.T) {
	src := "package p\nfunc small() { if true { return } }\nfunc long() {\n" +
		strings.Repeat("\tif x := 1; x > 0 { _ = x }\n", maxFunctionBranches+1) + "}\n"
	f, err := parser.ParseFile(token.NewFileSet(), "p.go", src, 0)
	if err != nil {
		t.Fatal(err)
	}
	found := map[string]functionSize{}
	measureFile("internal/app/p/p.go", f, found)
	if got := found["internal/app/p/p.go\tsmall"]; got != (functionSize{statements: 2, branches: 1}) {
		t.Fatalf("small measured %+v", got)
	}
	long := found["internal/app/p/p.go\tlong"]
	if long.branches != maxFunctionBranches+1 {
		t.Fatalf("long measured %+v", long)
	}

	problems, _ := functionProblems(found, nil)
	assertProblems(t, problems, []string{"internal/app/p/p.go long has"})

	grown := fmt.Sprintf("internal/app/p/p.go\tlong\t%d\t%d", long.statements-1, long.branches)
	problems, _ = functionProblems(found, []string{grown})
	assertProblems(t, problems, []string{"grew to"})

	roomy := fmt.Sprintf("internal/app/p/p.go\tlong\t%d\t%d", long.statements+5, long.branches)
	problems, tightened := functionProblems(found, []string{roomy, "internal/app/p/p.go\tgone\t200\t50", "internal/app/p/p.go\tsmall\t200\t50"})
	assertProblems(t, problems, []string{"shrank to", "names a function that is gone", "now fits the limits"})
	if want := fmt.Sprintf("internal/app/p/p.go\tlong\t%d\t%d", long.statements, long.branches); len(tightened) != 1 || tightened[0] != want {
		t.Errorf("tightened = %q, want %q", tightened, want)
	}
}

func TestTheFunctionGuardDoesNotCountADataTable(t *testing.T) {
	src := "package p\nfunc table() []int {\n\treturn []int{\n" + strings.Repeat("\t\t1,\n", 1000) + "\t}\n}\n"
	f, err := parser.ParseFile(token.NewFileSet(), "p.go", src, 0)
	if err != nil {
		t.Fatal(err)
	}
	found := map[string]functionSize{}
	measureFile("p.go", f, found)
	if got := found["p.go\ttable"]; got.over() {
		t.Fatalf("a 1000-line literal measured as %+v", got)
	}
}

func assertProblems(t *testing.T, problems, want []string) {
	t.Helper()
	if len(problems) != len(want) {
		t.Fatalf("problems = %q, want one for each of %q", problems, want)
	}
	for _, w := range want {
		hit := false
		for _, p := range problems {
			if strings.Contains(p, w) {
				hit = true
			}
		}
		if !hit {
			t.Errorf("no problem says %q: %q", w, problems)
		}
	}
}
