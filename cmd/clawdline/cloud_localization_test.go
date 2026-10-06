package main

import (
	"encoding/json"
	"io"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/sainteye/clawdline/internal/adapters/cloud"
	cloudtransport "github.com/sainteye/clawdline/internal/transport/cloud"
)

func captureCloudStream(t *testing.T, stream **os.File, fn func()) string {
	t.Helper()
	reader, writer, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	previous := *stream
	*stream = writer
	defer func() { *stream = previous }()
	fn()
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	data, err := io.ReadAll(reader)
	reader.Close()
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

func TestCloudCatalogHasCompleteNineLanguageCopy(t *testing.T) {
	for _, language := range []string{"en", "zh-Hant", "ja", "zh-Hans", "ko", "es", "pt-BR", "fr", "de"} {
		translated, total := cliCatalogCoverage("cloud", language)
		if total != 88 || translated != total {
			t.Errorf("cloud coverage %s = %d/%d, want 88/88", language, translated, total)
		}
	}
}

func TestCloudOutputTranslatesGermanAndTraditionalChineseWithoutChangingWireData(t *testing.T) {
	previous := commandLanguage
	t.Cleanup(func() { commandLanguage = previous })
	state := cloudtransport.PairingState{
		Phase: cloudtransport.PairingPaired, ViewerDeviceID: "viewer-raw-id",
		ViewerFingerprint: "viewer-raw-key", MachineFingerprint: "machine-raw-key",
	}
	for _, tc := range []struct{ language, usage, paired, access, status string }{
		{"de", "Verwendung:", "gekoppelt", "dieser Browser kann jetzt lesen", "Status"},
		{"zh-Hant", "用法：", "已配對", "該瀏覽器現在可以讀取", "狀態"},
	} {
		commandLanguage = tc.language
		usage := captureCloudStream(t, &os.Stderr, cloudUsage)
		if !strings.Contains(usage, tc.usage) || !strings.Contains(usage, "clawdline cloud <status|preflight|on|off|commands|login|pair|devices|revoke|rotate|connect>") {
			t.Errorf("%s usage lost translation or command names: %q", tc.language, usage)
		}
		pairing := captureCloudStream(t, &os.Stdout, func() { printPairingState(state) })
		for _, want := range []string{tc.paired, tc.access, state.ViewerDeviceID, state.ViewerFingerprint, state.MachineFingerprint} {
			if !strings.Contains(pairing, want) {
				t.Errorf("%s pairing lacks %q: %q", tc.language, want, pairing)
			}
		}
		recorder := cloud.NewStatusRecorder(time.Unix(100, 0))
		human := captureCloudStream(t, &os.Stdout, func() { reportCloudExit(nil, recorder, false) })
		if !strings.Contains(human, tc.status) || !strings.Contains(human, cloud.StateIdle) {
			t.Errorf("%s status lost translated label or raw state: %q", tc.language, human)
		}
	}
	recorder := cloud.NewStatusRecorder(time.Unix(100, 0))
	commandLanguage = "de"
	germanJSON := captureCloudStream(t, &os.Stdout, func() { reportCloudExit(nil, recorder, true) })
	commandLanguage = "zh-Hant"
	chineseJSON := captureCloudStream(t, &os.Stdout, func() { reportCloudExit(nil, recorder, true) })
	if germanJSON != chineseJSON {
		t.Fatalf("cloud JSON changed with language: German %q, Chinese %q", germanJSON, chineseJSON)
	}
	var fields map[string]any
	if err := json.Unmarshal([]byte(germanJSON), &fields); err != nil {
		t.Fatal(err)
	}
	if fields["state"] != cloud.StateIdle || fields["enabled"] != false || fields["connects"] != float64(0) {
		t.Fatalf("cloud JSON lost wire fields: %#v", fields)
	}
}
