package http

import (
	"net/http/httptest"
	"os"
	"regexp"
	"strings"
	"testing"

	"github.com/sainteye/clawdline-go/internal/app/orchestrator"
)

// A remedy names a route this daemon serves and a credential its gate reads
// (broker-design #37). The Swift app's printed curl sent the landing route a
// header the server did not read (`c9e89ce6`); here the table remedies are
// built from is checked against the router's registrations and the gate's
// own header names, so that cannot be written without a test going red.
func TestEveryRemedyNamesARouteThisDaemonServes(t *testing.T) {
	if machineHeader != orchestrator.HeaderMachine {
		t.Fatalf("the gate reads %q, remedies send %q", machineHeader, orchestrator.HeaderMachine)
	}
	req := httptest.NewRequest("GET", "/", nil)
	req.Header.Set(orchestrator.HeaderTaskSecret, "x")
	if taskSecret(req) != "x" {
		t.Fatalf("the task routes do not read %q", orchestrator.HeaderTaskSecret)
	}
	src, err := os.ReadFile("server.go")
	if err != nil {
		t.Fatal(err)
	}
	patterns := []string{}
	for _, m := range regexp.MustCompile(`mux\.HandleFunc\("([^"]+)"`).FindAllStringSubmatch(string(src), -1) {
		patterns = append(patterns, m[1])
	}
	served := func(route string) bool {
		for _, p := range patterns {
			if p == route || (strings.HasSuffix(p, "/") && strings.HasPrefix(route, p)) {
				return true
			}
		}
		return false
	}
	if len(orchestrator.Remedies) == 0 {
		t.Fatal("no remedies")
	}
	for code, r := range orchestrator.Remedies {
		if !served(r.Route) {
			t.Errorf("%s names %s, which no mux registration serves", code, r.Route)
		}
		if r.Header == orchestrator.HeaderMachine && !machineScoped(r.Route) {
			t.Errorf("%s sends the orchestrator token to %s, which the gate does not accept it on", code, r.Route)
		}
	}
	// The control: a route nobody registered is caught.
	if served("/v1/orchestrator/no-such-route") {
		t.Fatal("the check passes a route that does not exist")
	}
}
