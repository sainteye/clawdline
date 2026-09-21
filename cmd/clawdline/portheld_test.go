package main

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"os"
	"runtime"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/sainteye/clawdline-go/internal/adapters/process"
)

// The morning of 2026-09-21 in one process: something holds the port, it is a
// Clawdline daemon, it is alive, and its `/` refuses. The daemon that loses the
// port has to say all four, and the holder is found by the real process table.
func TestADaemonThatCannotBindNamesWhoHasThePort(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("the holder's start time is not asked of Windows; the parsers are tested in internal/adapters/process")
	}
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	holder := &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path == "/v1/health" {
			fmt.Fprint(w, `{"ok":true,"served_by":"clawdline-go"}`)
			return
		}
		w.WriteHeader(http.StatusNotImplemented)
		fmt.Fprint(w, `{"error":"no_web_root","detail":"this daemon was not told where the console is"}`)
	})}
	go func() { _ = holder.Serve(ln) }()
	t.Cleanup(func() { _ = holder.Close() })
	port := ln.Addr().(*net.TCPAddr).Port

	// The second daemon, as `serve` binds.
	_, bindErr := net.Listen("tcp", "127.0.0.1:"+strconv.Itoa(port))
	if !errors.Is(bindErr, syscall.EADDRINUSE) {
		t.Fatalf("the second bind: %v", bindErr)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	sentence, held := heldPortSentence(ctx, "127.0.0.1", port, bindErr, process.HolderOf, askPort)
	if !held {
		t.Fatalf("not held: %s", sentence)
	}
	for _, want := range []string{
		"port " + strconv.Itoa(port) + " on 127.0.0.1 is already held by pid " + strconv.Itoa(os.Getpid()),
		"running since " + time.Now().Format("2006-01-02"),
		"as clawdline-go",
		"/ answers 501 no_web_root: it serves no console",
		"has started nothing",
		"Stop pid " + strconv.Itoa(os.Getpid()) + " by its PID",
	} {
		if !strings.Contains(sentence, want) {
			t.Errorf("the sentence does not say %q:\n%s", want, sentence)
		}
	}
}

// A holder the system will not name, and one that is not a Clawdline daemon,
// are both said as what they are rather than as silence.
func TestAHolderNobodyCanNameIsStillSaid(t *testing.T) {
	inUse := &net.OpError{Op: "listen", Err: os.NewSyscallError("bind", syscall.EADDRINUSE)}
	unknown := func(context.Context, int) (process.Holder, error) {
		return process.Holder{}, errors.New("lsof: exit status 2")
	}
	notOurs := func(context.Context, string) (int, []byte, error) {
		return http.StatusNotFound, []byte("<html>nginx</html>"), nil
	}
	sentence, held := heldPortSentence(context.Background(), "127.0.0.1", 7727, inUse, unknown, notOurs)
	if !held || !strings.Contains(sentence, "would not say by whom: lsof: exit status 2") ||
		!strings.Contains(sentence, "It is not a Clawdline daemon") {
		t.Fatalf("%v: %s", held, sentence)
	}

	// A bind that failed for another reason is that reason, and not "held".
	denied := &net.OpError{Op: "listen", Err: os.NewSyscallError("bind", syscall.EACCES)}
	nobody := func(context.Context, int) (process.Holder, error) { return process.Holder{}, process.ErrNoHolder }
	sentence, held = heldPortSentence(context.Background(), "127.0.0.1", 80, denied, nobody, notOurs)
	if held || !strings.Contains(sentence, "could not listen on port 80") {
		t.Fatalf("%v: %s", held, sentence)
	}
}
