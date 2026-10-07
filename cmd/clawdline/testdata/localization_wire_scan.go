package main

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

func name(expr ast.Expr) string {
	switch v := expr.(type) {
	case *ast.Ident:
		return v.Name
	case *ast.SelectorExpr:
		return name(v.X) + "." + v.Sel.Name
	case *ast.CallExpr:
		return name(v.Fun) + "()"
	}
	return fmt.Sprintf("%T", expr)
}
func inside(n ast.Node, expr ast.Expr) bool {
	return expr != nil && expr.Pos() <= n.Pos() && n.End() <= expr.End()
}
func localized(call *ast.CallExpr) bool {
	switch name(call.Fun) {
	case "cliCopy", "productcopy.Format", "scheduleNotice":
		return true
	}
	return false
}
func main() {
	root := "."
	if len(os.Args) > 1 {
		root = os.Args[1]
	}
	fset := token.NewFileSet()
	total, flagged := 0, 0
	for _, top := range []string{"cmd", "internal"} {
		err := filepath.WalkDir(filepath.Join(root, top), func(path string, d fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if d.IsDir() {
				return nil
			}
			if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
				return nil
			}
			file, e := parser.ParseFile(fset, path, nil, 0)
			if e != nil {
				return e
			}
			stack := []ast.Node{}
			ast.Inspect(file, func(n ast.Node) bool {
				if n == nil {
					stack = stack[:len(stack)-1]
					return false
				}
				if call, ok := n.(*ast.CallExpr); ok && localized(call) {
					total++
					contexts := []string{}
					for i := len(stack) - 1; i >= 0; i-- {
						switch p := stack[i].(type) {
						case *ast.KeyValueExpr:
							if inside(call, p.Key) {
								contexts = append(contexts, "MAP_KEY")
							}
						case *ast.IndexExpr:
							if inside(call, p.Index) {
								contexts = append(contexts, "INDEX_KEY")
							}
						case *ast.CallExpr:
							if len(contexts) == 0 {
								for j, arg := range p.Args {
									if inside(call, arg) {
										contexts = append(contexts, fmt.Sprintf("ARG%d(%s)", j, name(p.Fun)))
										break
									}
								}
							}
						case *ast.AssignStmt:
							if len(contexts) == 0 {
								for _, lhs := range p.Lhs {
									contexts = append(contexts, "ASSIGN_TO("+name(lhs)+")")
								}
							}
						}
					}
					kind := "human_or_review"
					for _, ctx := range contexts {
						if ctx == "MAP_KEY" || ctx == "INDEX_KEY" || strings.Contains(ctx, "Header.Set") || strings.Contains(ctx, "Query().Set") || strings.Contains(ctx, "url.Values.Set") || strings.Contains(ctx, "PathEscape") || strings.Contains(ctx, "QueryEscape") || strings.Contains(ctx, "NewRequest") || strings.Contains(ctx, "filepath.Join") || strings.Contains(ctx, "ASSIGN_TO(path)") || strings.Contains(ctx, "ASSIGN_TO(endpoint)") || strings.Contains(ctx, "ARG1(.request)") {
							kind = "WIRE_RISK"
						}
					}
					if kind == "WIRE_RISK" {
						flagged++
					}
					fmt.Printf("%s\t%s\t%s\t%s\n", fset.Position(call.Pos()), name(call.Fun), kind, strings.Join(contexts, ","))
				}
				stack = append(stack, n)
				return true
			})
			return nil
		})
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(2)
		}
	}
	fmt.Fprintf(os.Stderr, "catalog calls %d, structural wire risks %d\n", total, flagged)
	if flagged > 0 {
		os.Exit(1)
	}
}
