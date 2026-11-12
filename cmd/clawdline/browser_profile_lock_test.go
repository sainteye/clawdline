package main

import (
	"bufio"
	"context"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/sainteye/clawdline/internal/app/browserbridge"
)

func TestBrowserProfileLockProcessHelper(t *testing.T) {
	path := os.Getenv("CLAWDLINE_TEST_BROWSER_LOCK")
	if path == "" {
		t.Skip("subprocess helper")
	}
	lock, err := browserProfileLock(context.Background(), path)
	if err != nil {
		t.Fatal(err)
	}
	defer lock.Close()
	fmt.Println("locked")
	time.Sleep(time.Hour)
}

func TestBrowserProfileLockIsReleasedWhenItsProcessDies(t *testing.T) {
	path := filepath.Join(t.TempDir(), "host.lock")
	cmd := exec.Command(os.Args[0], "-test.run=^TestBrowserProfileLockProcessHelper$")
	cmd.Env = append(os.Environ(), "CLAWDLINE_TEST_BROWSER_LOCK="+path)
	out, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() { cmd.Process.Kill(); cmd.Wait() }()
	ready := make(chan string, 1)
	go func() { line, _ := bufio.NewReader(out).ReadString('\n'); ready <- line }()
	select {
	case line := <-ready:
		if line != "locked\n" {
			t.Fatal(line)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("helper did not acquire ownership")
	}
	lock, ok, err := tryBrowserProfileLock(path)
	if err != nil {
		t.Fatal(err)
	}
	if ok {
		lock.Close()
		t.Fatal("two processes acquired the profile")
	}
	if err := cmd.Process.Kill(); err != nil {
		t.Fatal(err)
	}
	if err := cmd.Wait(); err == nil {
		t.Fatal("helper was not killed")
	}
	lock, ok, err = tryBrowserProfileLock(path)
	if err != nil || !ok {
		t.Fatalf("dead process retained ownership: %v %v", ok, err)
	}
	lock.Close()
}

func TestBrowserProfileLockWaitCanBeCancelled(t *testing.T) {
	path := filepath.Join(t.TempDir(), "host.lock")
	lock, ok, err := tryBrowserProfileLock(path)
	if err != nil || !ok {
		t.Fatal(ok, err)
	}
	defer lock.Close()
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	done := make(chan error, 1)
	go func() {
		got, err := browserProfileLock(ctx, path)
		if got != nil {
			got.Close()
		}
		done <- err
	}()
	select {
	case err := <-done:
		t.Fatalf("waiter bypassed live owner: %v", err)
	case <-time.After(30 * time.Millisecond):
	}
	cancel()
	select {
	case err := <-done:
		if err != context.Canceled {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("cancelled standby kept waiting")
	}
}

func TestOverlappingBrowserHostsKeepUnacknowledgedReceiptsAcrossRestart(t *testing.T) {
	state := t.TempDir()
	t.Setenv("CLAWDLINE_NEXT_DIR", state)
	profile := filepath.Join(state, "profile")
	path := browserCloseSpoolPath(state, "isolated", profile)
	if err := saveBrowserCloseResults(path, json.RawMessage(`[{"id":"A","ok":true,"tab":5}]`)); err != nil {
		t.Fatal(err)
	}
	firstReplay, releaseFirst, firstPoll, secondSaved, thirdReplay := make(chan struct{}), make(chan struct{}), make(chan struct{}), make(chan struct{}), make(chan struct{})
	shutdown := make(chan struct{})
	var mu sync.Mutex
	twoCalls := 0
	twoPolls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v1/browser/host/poll" {
			io.Copy(io.Discard, r.Body)
			r.Body.Close()
		}
		host := r.Header.Get("X-Clawdline-Orchestrator")
		if host == "two" {
			mu.Lock()
			twoCalls++
			mu.Unlock()
		}
		if r.URL.Path == "/v1/browser/host/results" {
			var body struct{ Results []browserbridge.Result }
			json.NewDecoder(r.Body).Decode(&body)
			if len(body.Results) != 1 {
				t.Error("bad result batch")
				w.WriteHeader(400)
				return
			}
			id := body.Results[0].ID
			if host == "one" && id == "A" {
				close(firstReplay)
				select {
				case <-releaseFirst:
				case <-r.Context().Done():
					return
				}
			}
			if host == "two" && id == "B" {
				close(secondSaved)
				select {
				case <-r.Context().Done():
				case <-shutdown:
				}
				return
			}
			if host == "three" {
				if id != "B" {
					t.Error("wrong replay")
				}
				close(thirdReplay)
			}
			w.Write([]byte(`{"ok":true}`))
			return
		}
		if host == "one" {
			select {
			case <-firstPoll:
			default:
				close(firstPoll)
			}
		}
		if host == "two" {
			mu.Lock()
			twoPolls++
			n := twoPolls
			mu.Unlock()
			if n == 1 {
				w.Write([]byte(`{"commands":[{"id":"B","op":"close","tab":7}]}`))
				return
			}
		}
		select {
		case <-r.Context().Done():
		case <-shutdown:
		}
	}))
	defer func() { close(shutdown); server.Close() }()
	start := func(token string) (*io.PipeWriter, <-chan int) {
		input, writer := io.Pipe()
		output, sink := io.Pipe()
		done := make(chan int, 1)
		t.Cleanup(func() { writer.Close(); input.Close(); output.Close(); sink.Close() })
		go func() {
			for {
				raw, err := readNativeMessage(output)
				if err != nil {
					return
				}
				var msg struct{ Command browserbridge.Command }
				json.Unmarshal(raw, &msg)
				if msg.Command.ID == "B" {
					data := []byte(`{"results":[{"id":"B","ok":true,"tab":7}]}`)
					binary.Write(writer, binary.LittleEndian, uint32(len(data)))
					writer.Write(data)
				}
			}
		}()
		go func() {
			done <- relayBrowserHost(&broker{base: server.URL, token: token}, "isolated", profile, input, sink, io.Discard)
		}()
		return writer, done
	}
	wait := func(ch <-chan struct{}, why string) {
		t.Helper()
		select {
		case <-ch:
		case <-time.After(6 * time.Second):
			t.Fatal(why)
		}
	}
	stop := func(w *io.PipeWriter, done <-chan int) {
		t.Helper()
		w.Close()
		select {
		case code := <-done:
			if code != 0 {
				t.Fatal(code)
			}
		case <-time.After(time.Second):
			t.Fatal("host did not stop")
		}
	}
	one, doneOne := start("one")
	defer one.Close()
	wait(firstReplay, "first replay missing")
	two, doneTwo := start("two")
	defer two.Close()
	time.Sleep(50 * time.Millisecond)
	mu.Lock()
	n := twoCalls
	mu.Unlock()
	if n != 0 {
		t.Fatal("standby host touched receipts while first host owned the profile")
	}
	close(releaseFirst)
	wait(firstPoll, "first replay was not acknowledged")
	mu.Lock()
	n = twoCalls
	mu.Unlock()
	if n != 0 {
		t.Fatal("ownership ended at ACK instead of at host exit")
	}
	stop(one, doneOne)
	wait(secondSaved, "standby did not resume after owner exited")
	raw, err := loadBrowserCloseResults(path)
	if err != nil || !bytesContainReceipt(raw, "B") {
		t.Fatalf("unacknowledged B missing: %s %v", raw, err)
	}
	stop(two, doneTwo)
	raw, err = loadBrowserCloseResults(path)
	if err != nil || !bytesContainReceipt(raw, "B") {
		t.Fatal("disconnect lost B")
	}
	three, doneThree := start("three")
	defer three.Close()
	wait(thirdReplay, "B was not recovered by a new host")
	deadline := time.Now().Add(time.Second)
	for {
		_, err := os.Stat(path)
		if os.IsNotExist(err) {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("ACK did not clear B")
		}
		time.Sleep(time.Millisecond)
	}
	stop(three, doneThree)
}

func bytesContainReceipt(raw json.RawMessage, id string) bool {
	var rows []browserbridge.Result
	if json.Unmarshal(raw, &rows) != nil {
		return false
	}
	return len(rows) == 1 && rows[0].ID == id
}
