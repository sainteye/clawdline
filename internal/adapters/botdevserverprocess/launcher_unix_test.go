//go:build darwin || linux || freebsd || openbsd || netbsd || dragonfly

package botdevserverprocess

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	pa "github.com/sainteye/clawdline/internal/domain/projectassistant"
)

func TestForegroundServerPeer(t *testing.T) {
	if os.Args[len(os.Args)-1] != "--synthetic-foreground-server" {
		return
	}
	listener, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		os.Exit(8)
	}
	fmt.Fprintln(os.Stdout, listener.Addr().String())
	fmt.Fprintln(os.Stderr, "synthetic child stderr")
	server := &http.Server{ReadHeaderTimeout: time.Second, Handler: http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		pwd, _ := os.Getwd()
		secret := false
		for _, key := range []string{"OPENAI_API_KEY", "ANTHROPIC_API_KEY", "SYNTHETIC_PROVIDER_SECRET", "CLAWDLINE_NEXT_DIR", "NODE_OPTIONS", "BASH_ENV", "ENV"} {
			secret = secret || os.Getenv(key) != ""
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"directory": pwd, "home": os.Getenv("HOME"), "secret_inherited": secret})
	})}
	stopped := make(chan struct{})
	go func() {
		_, _ = bufio.NewReader(os.Stdin).ReadString('\n')
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		_ = server.Shutdown(ctx)
		close(stopped)
	}()
	_ = server.Serve(listener)
	<-stopped
	os.Exit(0)
}

func shellQuote(text string) string { return "'" + strings.ReplaceAll(text, "'", "'\"'\"'") + "'" }

func TestSourceCommandHasItsOwnEnvironmentDirectoryOutputAndCleanup(t *testing.T) {
	project, spec := projectFixture(t)
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	spec.Command = "exec " + shellQuote(executable) + " -test.run=^TestForegroundServerPeer$ -- --synthetic-foreground-server"
	t.Setenv("SYNTHETIC_PROVIDER_SECRET", "synthetic-must-not-be-inherited")
	l, err := New(limits(), []string{"PATH=/usr/bin:/bin", "HOME=" + project.Directory, "LANG=C", "OPENAI_API_KEY=synthetic", "NODE_OPTIONS=synthetic"})
	if err != nil {
		t.Fatal(err)
	}
	request, cancel := context.WithCancel(context.Background())
	p, err := l.Start(request, spec, project)
	if p != nil {
		t.Cleanup(func() {
			ctx, cancel := context.WithTimeout(context.Background(), 6*time.Second)
			defer cancel()
			if err := p.Close(ctx); err != nil {
				t.Error(err)
			}
		})
	}
	if err != nil {
		t.Fatal(err)
	}
	reader := bufio.NewReader(p.Output())
	address, err := reader.ReadString('\n')
	if err != nil {
		t.Fatal(err)
	}
	stderr, err := reader.ReadString('\n')
	if err != nil || stderr != "synthetic child stderr\n" {
		t.Fatalf("combined stderr=%q %v", stderr, err)
	}
	cancel()
	client := &http.Client{Timeout: time.Second}
	url := "http://" + strings.TrimSpace(address) + "/"
	response, err := client.Get(url)
	if err != nil {
		t.Fatal("request cancellation stopped the command", err)
	}
	var got struct {
		Directory string `json:"directory"`
		Home      string `json:"home"`
		Secret    bool   `json:"secret_inherited"`
	}
	err = json.NewDecoder(io.LimitReader(response.Body, 1<<20)).Decode(&got)
	response.Body.Close()
	if err != nil || got.Directory != project.Directory || got.Home != project.Directory || got.Secret {
		t.Fatalf("source command environment/directory=%+v %v", got, err)
	}
	cleanup, stop := context.WithTimeout(context.Background(), 6*time.Second)
	defer stop()
	if err := p.Close(cleanup); err != nil {
		t.Fatal(err)
	}
	if _, err := client.Get(url); err == nil {
		t.Fatal("owned cleanup left the HTTP listener accepting")
	}
	if err := p.WaitExit(cleanup); err != nil {
		t.Fatal("graceful cleanup lost the direct child's success receipt", err)
	}
}

func TestRetargetedOrForeignCommandDirectoryNeverStarts(t *testing.T) {
	project, spec := projectFixture(t)
	l, err := New(limits(), nil)
	if err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(project.Directory, "linked")
	if err := os.Symlink(t.TempDir(), link); err != nil {
		t.Fatal(err)
	}
	if _, err := ResolveDirectory(project, "linked"); pa.Code(err) != pa.ScopeDenied {
		t.Fatal(err)
	}
	spec.Directory = link
	if p, err := l.Start(context.Background(), spec, project); p != nil || pa.Code(err) != pa.ScopeDenied {
		t.Fatalf("escaped startup acquired a handle: %v %v", p, err)
	}
	spec.Directory = project.Directory
	foreign := project
	foreign.Scope.CanonicalID = "another-repository"
	if p, err := l.Start(context.Background(), spec, foreign); p != nil || pa.Code(err) != pa.ScopeDenied {
		t.Fatalf("foreign Project acquired a handle: %v %v", p, err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if p, err := l.Start(ctx, spec, project); p != nil || err != context.Canceled {
		t.Fatalf("cancelled startup acquired a handle: %v %v", p, err)
	}
}

func TestRegisteredDirectoryAliasStillResolvesTheSameReviewedChild(t *testing.T) {
	project, _ := projectFixture(t)
	child := filepath.Join(project.Directory, "child")
	if err := os.Mkdir(child, 0o700); err != nil {
		t.Fatal(err)
	}
	alias := filepath.Join(t.TempDir(), "project-alias")
	if err := os.Symlink(project.Directory, alias); err != nil {
		t.Fatal(err)
	}
	project.Directory = alias
	for _, folder := range []string{"child", filepath.Join(alias, "child"), child} {
		if got, err := ResolveDirectory(project, folder); err != nil || got != child {
			t.Fatalf("registered alias changed the reviewed directory: %s %v", got, err)
		}
	}
}
