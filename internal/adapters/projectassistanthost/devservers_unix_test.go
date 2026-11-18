//go:build !windows

package projectassistanthost

import (
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	app "github.com/sainteye/clawdline/internal/app/projectassistant"
	pa "github.com/sainteye/clawdline/internal/domain/projectassistant"
)

// Only a separately launched Go test subprocess serves this synthetic peer.
func TestHostSourceServerHTTPPeer(t *testing.T) {
	args := os.Args
	if len(args) < 3 || args[len(args)-2] != "clawdline-source-http-peer" {
		return
	}
	listener, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		os.Exit(71)
	}
	if err := os.WriteFile(args[len(args)-1], []byte(listener.Addr().String()), 0600); err != nil {
		os.Exit(72)
	}
	fmt.Println("synthetic source HTTP ready")
	_ = http.Serve(listener, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { _, _ = io.WriteString(w, "synthetic source response") }))
	os.Exit(0)
}

func TestHostRealSourceProcessSurvivesNativeTurnAndClosesItsHTTPPort(t *testing.T) {
	f := newHostFixture(t)
	f.start(t)
	f.enable(t)
	bot := f.create(t, "source-http")
	tools, session := beginServerNative(t, f, bot, "start")
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	addressFile := filepath.Join(f.repo, "synthetic-address")
	quote := func(s string) string { return "'" + strings.ReplaceAll(s, "'", "'\\''") + "'" }
	command := "exec " + quote(executable) + " -test.run=^TestHostSourceServerHTTPPeer$ -- clawdline-source-http-peer " + quote(addressFile)
	done := make(chan error, 1)
	go func() {
		_, err := tools.DevServers(context.Background(), app.DevServerRequest{Action: "start", Key: "actual-http-start", Name: "Synthetic source command", Command: command})
		done <- err
	}()
	offer := hostSourceOffer(t, f, bot.Key)
	service, _ := f.h.Service()
	if _, err := service.Answer(context.Background(), pa.Command{Key: bot.Key, Operation: pa.AnswerOperation, IdempotencyKey: "allow-http"}, pa.PermissionAnswer{Fence: offer.Fence, TurnID: offer.TurnID, OfferID: offer.ID, OptionID: string(pa.AllowOnce)}); err != nil {
		t.Fatal(err)
	}
	if err := fixtureReceive(t, done); err != nil {
		t.Fatal(err)
	}
	var address string
	fixtureEventually(t, func() bool {
		b, err := os.ReadFile(addressFile)
		address = string(b)
		return err == nil && address != ""
	})
	client := &http.Client{Timeout: time.Second, Transport: &http.Transport{Proxy: nil}}
	t.Cleanup(client.CloseIdleConnections)
	read := func() string {
		response, err := client.Get("http://" + address)
		if err != nil {
			t.Fatal(err)
		}
		defer response.Body.Close()
		b, err := io.ReadAll(response.Body)
		if err != nil {
			t.Fatal(err)
		}
		return string(b)
	}
	if got := read(); got != "synthetic source response" {
		t.Fatal(got)
	}
	session.reply <- app.PromptResult{FinalText: "Source is serving"}
	fixtureEventually(t, func() bool { return f.h.Usage().ActiveActors == 0 })
	if got := read(); got != "synthetic source response" {
		t.Fatal("native close killed the independent source process", got)
	}
	if err := f.h.Disable(context.Background()); err != nil {
		t.Fatal(err)
	}
	client.CloseIdleConnections()
	if response, err := client.Get("http://" + address); err == nil {
		response.Body.Close()
		t.Fatal("verified source cleanup left its TCP listener reachable")
	}
}
