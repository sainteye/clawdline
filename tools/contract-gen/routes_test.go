package main

import (
	"strings"
	"testing"

	httptransport "github.com/sainteye/clawdline/internal/transport/http"
)

func table(patterns ...string) []httptransport.Route {
	out := []httptransport.Route{}
	for _, p := range patterns {
		out = append(out, httptransport.Route{Method: "*", Pattern: p})
	}
	return out
}

func written(level int, patterns ...string) *routesFile {
	f := &routesFile{APILevel: level}
	for _, p := range patterns {
		f.Routes = append(f.Routes, routeRow{Method: "*", Pattern: p, Since: 1})
	}
	return f
}

// The level is how a console tells two daemons apart, so a route set that
// changed under an unchanged level is refused, added and removed alike.
func TestARouteSetThatChangedMustRaiseTheLevel(t *testing.T) {
	prior := written(1, "/v1/a", "/v1/b")
	for name, next := range map[string][]httptransport.Route{
		"added":   table("/v1/a", "/v1/b", "/v1/c"),
		"removed": table("/v1/a"),
	} {
		_, err := planRoutes(prior, next, 1)
		if err == nil || !strings.Contains(err.Error(), "api_level did not rise") {
			t.Errorf("%s at the same level: %v", name, err)
		}
	}
}

func TestARaisedLevelRecordsWhenEachRouteAppeared(t *testing.T) {
	got, err := planRoutes(written(1, "/v1/a"), table("/v1/b", "/v1/a"), 2)
	if err != nil {
		t.Fatal(err)
	}
	if got.APILevel != 2 || len(got.Routes) != 2 ||
		got.Routes[0] != (routeRow{Method: "*", Pattern: "/v1/a", Since: 1}) ||
		got.Routes[1] != (routeRow{Method: "*", Pattern: "/v1/b", Since: 2}) {
		t.Errorf("%+v", got)
	}
}

func TestAnUnchangedSetAtTheSameLevelIsCurrent(t *testing.T) {
	if _, err := planRoutes(written(3, "/v1/a"), table("/v1/a"), 3); err != nil {
		t.Error(err)
	}
}

func TestALevelNeverGoesDown(t *testing.T) {
	if _, err := planRoutes(written(3, "/v1/a"), table("/v1/a"), 2); err == nil {
		t.Error("a lower level was accepted")
	}
	if _, err := planRoutes(nil, table("/v1/a"), 0); err == nil {
		t.Error("level 0 was accepted")
	}
}

func TestATableThatRegistersAPatternTwiceIsRefused(t *testing.T) {
	if _, err := planRoutes(nil, table("/v1/a", "/v1/a"), 1); err == nil {
		t.Error("a duplicate was accepted")
	}
}
