package main

import (
	"bytes"
	"strings"
	"testing"

	"github.com/sainteye/clawdline/internal/contract"
)

func TestTunnelGermanTableKeepsDaemonStatusAndCommand(t *testing.T) {
	previous := commandLanguage
	commandLanguage = "de"
	t.Cleanup(func() { commandLanguage = previous })
	var out bytes.Buffer
	printTunnelStatus(&out, contract.TunnelStatus{
		State: "failed", Mode: "named", URL: "https://example.test",
		Reason: "raw daemon reason", Attempts: 3, Installed: false,
		Config: "/private/tunnel.yml", Command: []string{"cloudflared", "--config", "/private/tunnel.yml"},
	})
	got := out.String()
	for _, want := range []string{"Status", "failed", "Modus", "named",
		"raw daemon reason", "3", "cloudflared ist nicht installiert",
		"/private/tunnel.yml", "cloudflared --config /private/tunnel.yml"} {
		if !strings.Contains(got, want) {
			t.Fatalf("German tunnel output omitted %q: %q", want, got)
		}
	}
	if strings.Contains(got, "cloudflared is not installed") {
		t.Fatalf("English fixed text remained in German output: %q", got)
	}
}
