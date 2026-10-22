package main

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/json"
	"io"
	"net"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/sainteye/clawdline/internal/adapters/antigravity"
)

func TestProviderObserveRejectsArgumentsAndFailureWithoutStdout(t *testing.T) {
	previous := commandLanguage
	t.Cleanup(func() { commandLanguage = previous })
	for _, lang := range []string{"en", "zh-Hant"} {
		commandLanguage = lang
		for _, args := range [][]string{nil, {"antigravity", "--event"}, {"antigravity", "--pid", "123"}, {"antigravity", "--event", "PreInvocation", "--capability", "private"}} {
			var out, errs bytes.Buffer
			if code := runProviderObserve(context.Background(), args, "/synthetic-absent", nil, strings.NewReader(`{}`), &out, &errs); code != 2 || out.Len() != 0 || errs.Len() == 0 {
				t.Fatalf("invalid args code/output: %d %q %q", code, out.String(), errs.String())
			}
		}
		var out, errs bytes.Buffer
		if code := runProviderObserve(context.Background(), []string{"antigravity", "--event", "PreInvocation"}, "/synthetic-absent", nil, strings.NewReader(`{}`), &out, &errs); code != 1 || out.Len() != 0 || strings.Contains(errs.String(), "/synthetic-absent") {
			t.Fatalf("failed hook code/output: %d %q %q", code, out.String(), errs.String())
		}
		if lang == "zh-Hant" && !strings.Contains(errs.String(), "失敗") {
			t.Fatal("Traditional Chinese failure missing")
		}
	}
}

func TestProviderObserveConfiguredCapabilityOneAttempt(t *testing.T) {
	if runtime.GOOS != "darwin" && runtime.GOOS != "linux" {
		t.Skip("Unix hooks")
	}
	root, err := os.MkdirTemp("/private/tmp", "ag-cli-")
	if err != nil {
		root, err = os.MkdirTemp("/tmp", "ag-cli-")
	}
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(root)
	state := filepath.Join(root, "antigravity")
	id := "74000000-0000-4000-8000-000000000003"
	dir := filepath.Join(state, "launches", id)
	if err = os.MkdirAll(dir, 0700); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "capability")
	token := strings.Repeat("ab", 32)
	if err = os.WriteFile(path, []byte(token), 0600); err != nil {
		t.Fatal(err)
	}
	t.Setenv(antigravity.CapabilityFileEnv, path)
	listener, err := net.ListenUnix("unix", &net.UnixAddr{Name: antigravity.HookSocketPath(state), Net: "unix"})
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	if err = os.Chmod(antigravity.HookSocketPath(state), 0600); err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() {
		conn, err := listener.AcceptUnix()
		if err != nil {
			done <- err
			return
		}
		defer conn.Close()
		conn.SetDeadline(time.Now().Add(time.Second))
		var size [4]byte
		if _, err = io.ReadFull(conn, size[:]); err != nil {
			done <- err
			return
		}
		n := binary.BigEndian.Uint32(size[:])
		if n > antigravity.MaxHookFrameBytes {
			done <- io.ErrShortBuffer
			return
		}
		data := make([]byte, n)
		if _, err = io.ReadFull(conn, data); err != nil {
			done <- err
			return
		}
		var request struct {
			Launch *antigravity.HookLaunch `json:"launch"`
		}
		err = json.Unmarshal(data, &request)
		if err != nil || request.Launch == nil || request.Launch.ID != id || request.Launch.Capability != token {
			done <- io.ErrUnexpectedEOF
			return
		}
		reply := []byte(`{"version":1,"response":{}}`)
		binary.BigEndian.PutUint32(size[:], uint32(len(reply)))
		_, err = conn.Write(append(size[:], reply...))
		done <- err
	}()
	var out, errs bytes.Buffer
	args := []string{"observe", "antigravity", "--event", "PreInvocation"}
	if code := providerObserveCommand(context.Background(), args, root, strings.NewReader(`{}`), &out, &errs); code != 0 || out.String() != "{}\n" {
		t.Fatalf("configured command code=%d stderr=%q", code, errs.String())
	}
	if err = <-done; err != nil {
		t.Fatal(err)
	}
	// A malformed present capability must fail before connecting again.
	t.Setenv(antigravity.CapabilityFileEnv, "")
	out.Reset()
	errs.Reset()
	if code := providerObserveCommand(context.Background(), args, root, strings.NewReader(`{}`), &out, &errs); code != 1 || out.Len() != 0 || strings.Contains(errs.String(), path) || strings.Contains(errs.String(), token) {
		t.Fatal("invalid environment leaked or fell back")
	}
	listener.SetDeadline(time.Now().Add(20 * time.Millisecond))
	if conn, err := listener.AcceptUnix(); err == nil {
		conn.Close()
		t.Fatal("invalid capability made a second attempt")
	}
}
