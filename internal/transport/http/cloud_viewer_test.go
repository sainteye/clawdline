package http

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestLocalViewerRoutesRequireThisMachinesOwnDoor(t *testing.T) {
	h := newPushStandIn(t)
	for _, tc := range []struct {
		name, method, path, credential string
		want                           int
	}{
		{"paired browser cannot see viewer setup", http.MethodGet, "/v1/cloud/viewer/status", h.phone, http.StatusForbidden},
		{"paired browser cannot send through local viewer", http.MethodPost, "/v1/cloud/viewer/actions", h.phone, http.StatusForbidden},
		{"paired browser cannot read viewer skills", http.MethodGet, "/v1/cloud/viewer/skills?machine=m&session=s&generation=g", h.phone, http.StatusForbidden},
		{"machine token sees disabled viewer", http.MethodGet, "/v1/cloud/viewer/status", h.local, http.StatusOK},
		{"machine token cannot use disabled viewer", http.MethodGet, "/v1/cloud/viewer/machines", h.local, http.StatusConflict},
	} {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(tc.method, tc.path, nil)
			req.Host = "127.0.0.1:7757"
			req.Header.Set("Authorization", "Bearer "+tc.credential)
			if tc.method == http.MethodPost {
				req.Header.Set("Content-Type", "application/json")
			}
			response := httptest.NewRecorder()
			h.handler.ServeHTTP(response, req)
			if response.Code != tc.want {
				t.Fatalf("status %d, want %d: %s", response.Code, tc.want, response.Body.String())
			}
		})
	}
}
