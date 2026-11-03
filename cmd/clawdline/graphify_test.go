package main

import (
	"bytes"
	"context"
	"crypto/md5"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/sainteye/clawdline/internal/adapters/graphify"
)

func TestGraphifyCheckRefusesMissingIndexAndInvalidArguments(t *testing.T) {
	for _, args := range [][]string{{"check", t.TempDir()}, {"update", "."}, {"check"}, {"check", ".", "extra"}} {
		var out bytes.Buffer
		code := runGraphifyCheck(context.Background(), args, &out)
		var s graphify.Status
		if code == 0 || json.Unmarshal(out.Bytes(), &s) != nil || s.State == graphify.Matched || s.Reason == "" {
			t.Fatalf("args=%v code=%d body=%s", args, code, out.String())
		}
	}
}

func TestGraphifyCheckAcceptsMatchingNativeManifest(t *testing.T) {
	r := t.TempDir()
	var err error
	r, err = filepath.EvalSymlinks(r)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(r, "graphify-out"), 0700); err != nil {
		t.Fatal(err)
	}
	body := []byte("package demo\n")
	sum := md5.Sum(body)
	manifest, err := json.Marshal(map[string]any{"main.go": map[string]string{"ast_hash": hex.EncodeToString(sum[:])}})
	if err != nil {
		t.Fatal(err)
	}
	for name, data := range map[string][]byte{"main.go": body, "graphify-out/.graphify_root": []byte(r), "graphify-out/graph.json": []byte(`{"graph":{"schema_version":1},"nodes":[],"links":[]}`), "graphify-out/manifest.json": manifest} {
		if err := os.WriteFile(filepath.Join(r, name), data, 0600); err != nil {
			t.Fatal(err)
		}
	}
	var out bytes.Buffer
	code := runGraphifyCheck(context.Background(), []string{"check", r}, &out)
	var s graphify.Status
	if code != 0 || json.Unmarshal(out.Bytes(), &s) != nil || s.State != graphify.Matched {
		t.Fatalf("code=%d body=%s", code, out.String())
	}
}
