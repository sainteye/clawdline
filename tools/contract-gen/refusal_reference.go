package main

import (
	"bytes"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

var staticRefusalCode = regexp.MustCompile(`^[a-z][a-z0-9_]+$`)

// refusalReference reads literal typed refusal codes, not error prose. Domain
// constructors and transport writers carry the most consequential wire codes;
// a computed code is intentionally not guessed and must be handled as unknown.
func refusalReference(root string) (map[string][]string, error) {
	result := map[string]map[string]bool{}
	fset := token.NewFileSet()
	dirs := []string{
		filepath.Join(root, "internal", "transport", "http"),
		filepath.Join(root, "internal", "domain", "work"),
		filepath.Join(root, "internal", "app", "orchestrator"),
	}
	for _, dir := range dirs {
		entries, err := os.ReadDir(dir)
		if err != nil {
			return nil, err
		}
		for _, entry := range entries {
			if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".go") || strings.HasSuffix(entry.Name(), "_test.go") {
				continue
			}
			path := filepath.Join(dir, entry.Name())
			file, err := parser.ParseFile(fset, path, nil, 0)
			if err != nil {
				return nil, err
			}
			ast.Inspect(file, func(n ast.Node) bool {
				call, ok := n.(*ast.CallExpr)
				if !ok {
					return true
				}
				name := ""
				switch fun := call.Fun.(type) {
				case *ast.Ident:
					name = fun.Name
				case *ast.SelectorExpr:
					name = fun.Sel.Name
				}
				arg := -1
				switch name {
				case "writeRefusal", "writeRawRefusal", "writeAuthRefusal", "writeRawAuthRefusal", "writeRawRefusalAbout":
					arg = 2
				case "RefuseV2", "RefuseV2Raw":
					arg = 0
				}
				if arg >= 0 && len(call.Args) > arg {
					if lit, ok := call.Args[arg].(*ast.BasicLit); ok && lit.Kind == token.STRING {
						code, err := strconv.Unquote(lit.Value)
						if err == nil && staticRefusalCode.MatchString(code) {
							if result[code] == nil {
								result[code] = map[string]bool{}
							}
							result[code][filepath.ToSlash(strings.TrimPrefix(path, root+string(filepath.Separator)))] = true
						}
					}
				}
				return true
			})
		}
	}
	out := map[string][]string{}
	for code, sources := range result {
		for source := range sources {
			out[code] = append(out[code], source)
		}
		sort.Strings(out[code])
	}
	return out, nil
}

func renderRefusalReference(codes map[string][]string, traditionalChinese bool) []byte {
	var b bytes.Buffer
	if traditionalChinese {
		b.WriteString("# 此 build 的靜態拒絕碼\n\n此表由目前 Go 原始碼中的具型別拒絕呼叫產生，列出直接寫明的代碼及宣告處。動態產生的代碼不在此表，不能以此推斷不存在。收到拒絕時，以 `error` 欄位分支，不解析說明文字；執行 `clawdline guide refused <code>` 找處置規則。若目前指南沒有該代碼、狀態未知或收據不足，就停止相關寫入並回報，不自行重試。\n\n| 代碼 | 宣告處 |\n| --- | --- |\n")
	} else {
		b.WriteString("# Statically declared refusal codes in this build\n\nGenerated from typed refusal calls in the current Go source. It lists directly declared codes and their source files; dynamically computed codes are not included, so absence is not proof that a code cannot occur. Branch on the `error` field, never on prose. Run `clawdline guide refused <code>` for handling. If this build's guide has no rule for a code, or state or receipt evidence is unknown, stop the dependent write and report it; do not retry by guesswork.\n\n| Code | Declared in |\n| --- | --- |\n")
	}
	keys := make([]string, 0, len(codes))
	for code := range codes {
		keys = append(keys, code)
	}
	sort.Strings(keys)
	for _, code := range keys {
		fmt.Fprintf(&b, "| `%s` | `%s` |\n", code, strings.Join(codes[code], "`, `"))
	}
	return b.Bytes()
}
