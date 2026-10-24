package main

import (
	"bytes"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/sainteye/clawdline/internal/config"
	cloudtransport "github.com/sainteye/clawdline/internal/transport/cloud"
)

func TestPairRequestUsesVerifiedInstalledDaemonWhenShellStateHasNoToken(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/cloud/status" || r.Header.Get("Authorization") != "Bearer installed-token" {
			t.Errorf("verification request: %s %q", r.URL.Path, r.Header.Get("Authorization"))
			w.WriteHeader(http.StatusUnauthorized)
		}
	}))
	defer server.Close()
	_, portText, err := net.SplitHostPort(strings.TrimPrefix(server.URL, "http://"))
	if err != nil {
		t.Fatal(err)
	}
	port, err := strconv.Atoi(portText)
	if err != nil {
		t.Fatal(err)
	}
	installed := t.TempDir()
	if err := os.WriteFile(filepath.Join(installed, "local-token"), []byte("installed-token"), 0600); err != nil {
		t.Fatal(err)
	}
	stale := config.Config{Dir: t.TempDir()}
	staleListener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	stalePort := staleListener.Addr().(*net.TCPAddr).Port
	staleListener.Close()
	req, err := cloudPairRequestFor(http.MethodPost, "/v1/cloud/pairing", nil, stale, installed, stalePort, port)
	if err != nil {
		t.Fatal(err)
	}
	if req.URL.Path != "/v1/cloud/pairing" || req.Header.Get("Authorization") != "Bearer installed-token" {
		t.Fatalf("pair request: %s %q", req.URL.Path, req.Header.Get("Authorization"))
	}
	selected := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	defer selected.Close()
	_, selectedPortText, err := net.SplitHostPort(strings.TrimPrefix(selected.URL, "http://"))
	if err != nil {
		t.Fatal(err)
	}
	selectedPort, err := strconv.Atoi(selectedPortText)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := cloudPairRequestFor(http.MethodPost, "/v1/cloud/pairing", nil, stale, installed, selectedPort, port); err == nil {
		t.Fatal("a reachable selected daemon must not silently select the installed daemon")
	}
}

func TestCloudReadonlyPairingOutputNamesTheMachineGrantInBothLanguages(t *testing.T) {
	previous := commandLanguage
	t.Cleanup(func() { commandLanguage = previous })
	for language, want := range map[string]string{"en": "operating this machine is disabled", "zh-Hant": "不能操作這台機器"} {
		commandLanguage = language
		out := captureCloudStream(t, &os.Stdout, func() {
			printPairingState(cloudtransport.PairingState{Phase: cloudtransport.PairingPaired, Scope: "readonly", ViewerDeviceID: "synthetic-viewer"})
		})
		if !strings.Contains(out, want) || !strings.Contains(out, "synthetic-viewer") {
			t.Fatalf("%s: %s", language, out)
		}
	}
}

func TestCloudPairScopeHelpSaysAnOmittedScopeKeepsTheDevicesOwn(t *testing.T) {
	previous := commandLanguage
	t.Cleanup(func() { commandLanguage = previous })
	for language, want := range map[string][]string{
		"en":      {"already paired keeps its scope", "never-paired browser gets operate"},
		"zh-Hant": {"保留原權限", "從未配對的瀏覽器預設為 operate"},
	} {
		commandLanguage = language
		fs, _, _, _ := cloudPairFlags()
		var help bytes.Buffer
		fs.SetOutput(&help)
		fs.PrintDefaults()
		for _, phrase := range want {
			if !strings.Contains(help.String(), phrase) {
				t.Fatalf("%s help lacks %q:\n%s", language, phrase, help.String())
			}
		}
		if strings.Contains(help.String(), "compatibility") || strings.Contains(help.String(), "相容預設") {
			t.Fatalf("%s help still promises a compatibility default:\n%s", language, help.String())
		}
	}
}
