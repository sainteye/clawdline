package main

import (
	"context"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/sainteye/clawdline/internal/app/browserbridge"
)

func TestBrowserHostRetainsCloseResultUntilRestartedDaemonAcknowledges(t *testing.T) {
	state := t.TempDir()
	t.Setenv("CLAWDLINE_NEXT_DIR", state)
	if err := os.WriteFile(filepath.Join(state, browserbridge.TabsFile), []byte(`{"mode":"isolated","owner":{"5":"c1"},"current":{"c1":5}}`), 0600); err != nil {
		t.Fatal(err)
	}
	bridge, err := browserbridge.Open(state, nil)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	// Establish the synthetic host before the Agent queues close.
	ready, cancelReady := context.WithCancel(ctx)
	cancelReady()
	bridge.Poll(ready, browserbridge.HostInfo{Version: "fixture"})
	input, writer := io.Pipe()
	defer input.Close()
	defer writer.Close()
	output, readOutput := io.Pipe()
	defer output.Close()
	defer readOutput.Close()
	var mu sync.Mutex
	calls := 0
	acknowledged := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		b := bridge
		mu.Unlock()
		if r.URL.Path == "/v1/browser/host/poll" {
			wait, stop := context.WithTimeout(r.Context(), 30*time.Millisecond)
			defer stop()
			json.NewEncoder(w).Encode(map[string]any{"commands": b.Poll(wait, browserbridge.HostInfo{Version: "fixture"})})
			return
		}
		if r.URL.Path != "/v1/browser/host/results" {
			w.WriteHeader(404)
			return
		}
		var body struct{ Results []browserbridge.Result }
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Error(err)
			w.WriteHeader(400)
			return
		}
		mu.Lock()
		calls++
		n := calls
		mu.Unlock()
		if n == 1 {
			// The close executed, but the service was unavailable before its ACK.
			again, err := browserbridge.Open(state, nil)
			if err != nil {
				t.Error(err)
				w.WriteHeader(500)
				return
			}
			mu.Lock()
			bridge = again
			mu.Unlock()
			cancel()
			w.WriteHeader(503)
			return
		}
		if err := b.Deliver(body.Results); err != nil {
			t.Error(err)
			w.WriteHeader(503)
			return
		}
		w.Write([]byte(`{"ok":true}`))
		if n == 2 {
			close(acknowledged)
		}
	}))
	defer server.Close()
	b := &broker{base: server.URL, token: "synthetic", client: server.Client()}
	hostDone := make(chan int, 1)
	go func() {
		hostDone <- relayBrowserHost(b, "isolated", filepath.Join(state, "profile"), input, readOutput, io.Discard)
	}()
	go func() {
		for {
			raw, err := readNativeMessage(output)
			if err != nil {
				return
			}
			var msg struct{ Command browserbridge.Command }
			if json.Unmarshal(raw, &msg) != nil || msg.Command.ID == "" {
				continue
			}
			if msg.Command.Op != "close" {
				t.Error("unexpected command")
				return
			}
			result, _ := json.Marshal(map[string]any{"results": []browserbridge.Result{{ID: msg.Command.ID, OK: true, Tab: 5}}})
			binary.Write(writer, binary.LittleEndian, uint32(len(result)))
			writer.Write(result)
		}
	}()
	initial := bridge
	go func() { initial.Do(ctx, "c1", "close", nil) }()
	select {
	case <-acknowledged:
	case <-time.After(6 * time.Second):
		writer.Close()
		t.Fatal("host discarded unacknowledged close result")
	}
	deadline := time.Now().Add(time.Second)
	for {
		_, err := os.Stat(browserCloseSpoolPath(state, "isolated", filepath.Join(state, "profile")))
		if os.IsNotExist(err) {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("observed ACK did not clear spool")
		}
		time.Sleep(time.Millisecond)
	}
	writer.Close()
	select {
	case code := <-hostDone:
		if code != 0 {
			t.Fatal(code)
		}
	case <-time.After(time.Second):
		t.Fatal("host did not stop")
	}
	mu.Lock()
	again := bridge
	mu.Unlock()
	if len(again.Status().Tabs) != 0 {
		t.Fatal("confirmed closure remained after restart")
	}
	if err := again.SetMode("user_chrome"); err != nil {
		t.Fatalf("close result still blocks mode switch: %v", err)
	}
	if _, err := os.Stat(browserCloseSpoolPath(state, "isolated", filepath.Join(state, "profile"))); !os.IsNotExist(err) {
		t.Fatalf("acknowledged receipt retained: %v", err)
	}
}

func TestBrowserCloseSpoolContainsOnlyReceiptMetadata(t *testing.T) {
	closes := map[string]bool{"close": true}
	raw, err := browserCloseReceipts(json.RawMessage(`[{"id":"close","tab":5,"ok":true,"url":"https://example.com/private","target":"private","value":{"closed":true},"thumb":"picture"},{"id":"input","ok":true,"value":{"text":"secret"}}]`), closes)
	if err != nil {
		t.Fatal(err)
	}
	var got []map[string]any
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0]["id"] != "close" || len(got[0]) != 3 {
		t.Fatalf("spool retained page data: %s", raw)
	}
	path := filepath.Join(t.TempDir(), "receipts.json")
	if err := saveBrowserCloseResults(path, raw); err != nil {
		t.Fatal(err)
	}
	stored, err := loadBrowserCloseResults(path)
	if err != nil || string(stored) != string(raw) {
		t.Fatalf("reload: %s %v", stored, err)
	}
	info, _ := os.Stat(path)
	if info.Mode().Perm() != 0600 {
		t.Fatal(info.Mode())
	}
}

func TestBrowserHostReplaysSavedCloseReceiptAfterHostRestart(t *testing.T) {
	state := t.TempDir()
	t.Setenv("CLAWDLINE_NEXT_DIR", state)
	profile := filepath.Join(state, "profile")
	path := browserCloseSpoolPath(state, "isolated", profile)
	if err := saveBrowserCloseResults(path, json.RawMessage(`[{"id":"original-close-id","ok":true,"tab":5}]`)); err != nil {
		t.Fatal(err)
	}
	input, writer := io.Pipe()
	defer input.Close()
	defer writer.Close()
	received := make(chan string, 1)
	var orderMu sync.Mutex
	acked := false
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v1/browser/host/results" {
			var body struct{ Results []browserbridge.Result }
			json.NewDecoder(r.Body).Decode(&body)
			if len(body.Results) == 1 {
				received <- body.Results[0].ID
			}
			w.Write([]byte(`{"ok":true}`))
			orderMu.Lock()
			acked = true
			orderMu.Unlock()
			writer.Close()
			return
		}
		orderMu.Lock()
		ready := acked
		orderMu.Unlock()
		if !ready {
			t.Error("host polled before replaying its saved close receipts")
		}
		time.Sleep(10 * time.Millisecond)
		w.Write([]byte(`{"commands":[]}`))
	}))
	defer server.Close()
	done := make(chan int, 1)
	go func() {
		done <- relayBrowserHost(&broker{base: server.URL, client: server.Client()}, "isolated", profile, input, io.Discard, io.Discard)
	}()
	select {
	case id := <-received:
		if id != "original-close-id" {
			t.Fatal(id)
		}
	case <-time.After(time.Second):
		t.Fatal("saved close receipt was not replayed")
	}
	select {
	case code := <-done:
		if code != 0 {
			t.Fatal(code)
		}
	case <-time.After(time.Second):
		t.Fatal("host did not stop")
	}
}

func TestBrowserHostRefusesCloseOverflowWithoutDiscardingTheResult(t *testing.T) {
	state := t.TempDir()
	t.Setenv("CLAWDLINE_NEXT_DIR", state)
	input, writer := io.Pipe()
	defer input.Close()
	defer writer.Close()
	output, sink := io.Pipe()
	defer output.Close()
	defer sink.Close()
	received := make(chan browserbridge.Result, 1)
	var mu sync.Mutex
	polls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v1/browser/host/results" {
			var body struct{ Results []browserbridge.Result }
			json.NewDecoder(r.Body).Decode(&body)
			if len(body.Results) == 1 {
				received <- body.Results[0]
			}
			w.Write([]byte(`{"ok":true}`))
			return
		}
		mu.Lock()
		polls++
		n := polls
		mu.Unlock()
		var commands []browserbridge.Command
		if n == 1 {
			for i := 0; i < maxBrowserCloseReceipts; i++ {
				commands = append(commands, browserbridge.Command{ID: fmt.Sprint(i), Op: "close", Tab: i + 1})
			}
		}
		if n == 2 {
			commands = append(commands, browserbridge.Command{ID: "overflow", Op: "close", Tab: 99})
		}
		if n >= 3 {
			writer.Close()
		}
		json.NewEncoder(w).Encode(map[string]any{"commands": commands})
	}))
	defer server.Close()
	delivered := make(chan string, 1)
	go func() {
		for {
			raw, err := readNativeMessage(output)
			if err != nil {
				return
			}
			var msg struct{ Command browserbridge.Command }
			json.Unmarshal(raw, &msg)
			if msg.Command.ID == "overflow" {
				delivered <- msg.Command.ID
			}
		}
	}()
	done := make(chan int, 1)
	go func() {
		done <- relayBrowserHost(&broker{base: server.URL, client: server.Client()}, "isolated", filepath.Join(state, "profile"), input, sink, io.Discard)
	}()
	select {
	case r := <-received:
		if r.ID != "overflow" || r.Tab != 99 || r.Error != string(browserbridge.QueueFull) {
			t.Fatal(r)
		}
	case <-time.After(time.Second):
		writer.Close()
		t.Fatal("overflow close disappeared without a refusal result")
	}
	select {
	case code := <-done:
		if code != 0 {
			t.Fatal(code)
		}
	case <-time.After(time.Second):
		t.Fatal("host did not stop")
	}
	select {
	case <-delivered:
		t.Fatal("refused close was sent to Chrome")
	default:
	}
}
