package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRefusalReferenceReadsCodesAndNotMessages(t *testing.T) {
	root := t.TempDir()
	for _, dir := range []string{"internal/transport/http", "internal/domain/work", "internal/app/orchestrator"} {
		if err := os.MkdirAll(filepath.Join(root, dir), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	source := `package http
func f(w any) {
 writeRefusal(w, 409, "stale_version", "an explanatory message")
 writeRawRefusal(w, 429, "lane_full", "try later")
 writeRefusal(w, 500, computedCode, "not a literal")
}`
	if err := os.WriteFile(filepath.Join(root, "internal/transport/http/handler.go"), []byte(source), 0o644); err != nil {
		t.Fatal(err)
	}
	codes, err := refusalReference(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(codes) != 2 || len(codes["stale_version"]) != 1 || len(codes["lane_full"]) != 1 {
		t.Fatalf("codes = %v", codes)
	}
	body := string(renderRefusalReference(codes, false))
	for _, want := range []string{"stale_version", "lane_full", "guide refused <code>"} {
		if !strings.Contains(body, want) {
			t.Errorf("reference missing %q", want)
		}
	}
	if strings.Contains(body, "an explanatory message") || strings.Contains(body, "computedCode") {
		t.Fatal("reference treated prose or a dynamic expression as a stable code")
	}
}
