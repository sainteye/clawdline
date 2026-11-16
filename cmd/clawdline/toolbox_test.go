package main

import (
	"bytes"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestToolboxSearchReadsSavedAndRecommendedThroughMachineAuthority(t *testing.T) {
	for _, tc := range []struct {
		status int
		body   string
		want   int
	}{
		{200, `{"saved":[{"id":"custom","repo_url":"https://github.com/example/custom","summary":{"zh-Hant":"互動圖"}}],"catalog":{"projects":[{"id":"audit","repo_url":"https://github.com/example/audit","summary":{"en":"security"}}]}}`, 0},
		{200, `{}`, 1},
		{503, `{"error":"store_unavailable","detail":"Unavailable"}`, 1},
	} {
		seen := 0
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			seen++
			if r.Method != "GET" || r.URL.Path != "/v1/tool-library" || r.Header.Get("X-Clawdline-Orchestrator") != "test-token" {
				t.Error("wrong authority or mutation")
			}
			w.WriteHeader(tc.status)
			io.WriteString(w, tc.body)
		}))
		b := &broker{base: srv.URL, token: "test-token", client: srv.Client()}
		var out, errors bytes.Buffer
		code := runToolboxSearch(b, "互動", &out, &errors)
		srv.Close()
		if code != tc.want || seen != 1 {
			t.Fatal(code, seen, errors.String())
		}
		if code == 0 && (!strings.Contains(out.String(), `"id":"custom"`) || strings.Contains(out.String(), `"id":"audit"`)) {
			t.Fatal(out.String())
		}
	}
}
