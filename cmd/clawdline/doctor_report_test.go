package main

import (
	"bytes"
	"net/http"
	"strings"
	"testing"
)

func TestDoctorJSONUsesAuthenticatedDaemonAndKeepsRefusals(t *testing.T) {
	for _, status := range []int{200, 409} {
		_, b := newStandIn(t, func(r *http.Request) (int, string) {
			if r.Method != "GET" || r.URL.Path != "/v1/doctor" {
				t.Errorf("%s %s", r.Method, r.URL.Path)
			}
			if status == 200 {
				return 200, `{"schema":"clawdline.doctor/1","version":"dev-fixture"}`
			}
			return status, `{"error":{"code":"doctor_busy","message":"busy"}}`
		})
		var out, errOut bytes.Buffer
		code := readDoctorReport(&out, &errOut, b)
		if status == 200 && (code != 0 || !strings.Contains(out.String(), "dev-fixture")) {
			t.Fatalf("%d %s %s", code, &out, &errOut)
		}
		if status == 409 && code == 0 {
			t.Fatal("refusal called success")
		}
	}
}
