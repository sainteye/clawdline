package devstack

import (
	"context"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func write(t *testing.T, path, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestParseKeepsWhatItRecognises(t *testing.T) {
	spec, ok := Parse([]byte(`{
		"version": 1,
		"status": "make stack-status", "up": "make stack-up", "logs": "  ",
		"restart": "make stack-restart P={process}",
		"processes": [
			{"name": "api", "port": 8080, "url": "http://127.0.0.1:8080/admin/"},
			{"name": "web", "port": 3000.0},
			{"name": "", "port": 1},
			{"name": "worker"},
			{"name": "bad port", "port": 70000},
			"not an object"
		],
		"future": {"ignored": true}
	}`), "/work/shop")
	if !ok {
		t.Fatal("a valid file did not parse")
	}
	if spec.Name != "shop" {
		t.Errorf("name %q, want the directory's", spec.Name)
	}
	if got := strings.Join(spec.Commands, ","); got != "status,up,restart" {
		t.Errorf("commands %q: an empty one is not declared, and the order is fixed", got)
	}
	var got []string
	for _, p := range spec.Declared {
		got = append(got, p.Name+":"+itoa(p.Port))
	}
	if strings.Join(got, " ") != "api:8080 web:3000 worker:0 bad port:0" {
		t.Errorf("declared %v", got)
	}
	if spec.Declared[0].URL != "http://127.0.0.1:8080/admin/" {
		t.Errorf("url %q", spec.Declared[0].URL)
	}
	if _, ok := Parse([]byte(`[1,2]`), "/x"); ok {
		t.Error("a file that is not an object parsed")
	}
	named, _ := Parse([]byte("{\"name\":\"atr\\nium\"}"), "/x")
	if named.Name != "atr ium" {
		t.Errorf("a control character reached a row: %q", named.Name)
	}
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var b []byte
	for ; n > 0; n /= 10 {
		b = append([]byte{byte('0' + n%10)}, b...)
	}
	return string(b)
}

func TestDiscoverWalksUpDedupesAndSkipsCopies(t *testing.T) {
	home := t.TempDir()
	shop := filepath.Join(home, "code", "shop")
	blog := filepath.Join(home, "code", "blog")
	copyOf := filepath.Join(home, "worktrees", "shop-1")
	live := filepath.Join(home, "worktrees", "shop-2")
	plain := filepath.Join(home, "code", "plain")
	write(t, filepath.Join(shop, Filename), `{"name":"shop","processes":[{"name":"web","port":1}]}`)
	write(t, filepath.Join(blog, Filename), `{"status":"make status"}`)
	for _, wt := range []string{copyOf, live} {
		write(t, filepath.Join(wt, Filename), `{"name":"shop"}`)
		write(t, filepath.Join(wt, ".git"), "gitdir: /elsewhere\n")
	}
	if err := os.MkdirAll(plain, 0o755); err != nil {
		t.Fatal(err)
	}
	// Above home is never read: a file there is nobody's project.
	write(t, filepath.Join(filepath.Dir(home), Filename), `{"name":"outside"}`)

	d := Discover(
		[]string{filepath.Join(shop, "frontend", "src"), shop, blog, copyOf, plain, "relative/path"},
		[]string{live},
		home,
	)
	var roots []string
	for _, s := range d.Specs {
		roots = append(roots, s.Name+"@"+strings.TrimPrefix(s.Root, home))
	}
	if got, want := strings.Join(roots, " "), "blog@/code/blog shop@/code/shop shop@/worktrees/shop-2"; got != want {
		t.Fatalf("stacks = %q, want %q", got, want)
	}
	if d.Truncated {
		t.Error("truncated with nothing left out")
	}
}

func TestDiscoverSaysWhenItStopped(t *testing.T) {
	home := t.TempDir()
	var known []string
	for i := 0; i <= maxCandidates; i++ {
		known = append(known, filepath.Join(home, "p"))
	}
	if d := Discover(known, nil, home); !d.Truncated {
		t.Error("a walk past maxCandidates did not say so")
	}
}

func TestReadNeverCallsAnUnprobeableStackStopped(t *testing.T) {
	specs := []Spec{
		{Name: "all", Declared: []Process{{Name: "a", Port: 1, State: "stopped"}, {Name: "b", Port: 2, State: "stopped"}}},
		{Name: "some", Declared: []Process{{Name: "a", Port: 1, State: "stopped"}, {Name: "c", Port: 3, State: "stopped"}}},
		{Name: "none", Declared: []Process{{Name: "c", Port: 3, State: "stopped"}, {Name: "d", State: "stopped"}}},
		{Name: "status only", Commands: []string{"status", "up"}},
		{Name: "bare"},
	}
	up := map[int]bool{1: true, 2: true}
	got := Read(context.Background(), specs, func(_ context.Context, port int) bool { return up[port] })
	want := []string{"running", "partial", "stopped", "unknown/status_not_run", "unknown/nothing_declared"}
	for i, s := range got {
		verdict := s.State
		if s.Unknown != "" {
			verdict += "/" + s.Unknown
		}
		if verdict != want[i] {
			t.Errorf("%s: %s, want %s", s.Name, verdict, want[i])
		}
	}
	if got[1].Processes[0].State != "running" || got[1].Processes[1].State != "stopped" {
		t.Errorf("process states %+v", got[1].Processes)
	}
	if specs[0].Declared[0].State != "stopped" {
		t.Error("reading changed the spec it was handed")
	}
}

func TestListeningAsksTheRealPort(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Skipf("no loopback here: %v", err)
	}
	port := ln.Addr().(*net.TCPAddr).Port
	if !Listening(context.Background(), port) {
		t.Error("a listening port answered no")
	}
	ln.Close()
	if Listening(context.Background(), port) {
		t.Error("a closed port answered yes")
	}
	if Listening(context.Background(), 0) || Listening(context.Background(), 70000) {
		t.Error("a port out of range was asked")
	}
}
