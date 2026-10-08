package main

import (
	"strings"
	"testing"
)

func TestAgentRouteCatalogRequiresExactRegisteredCoverage(t *testing.T) {
	routes := routesFile{APILevel: 3, Routes: []routeRow{
		{Method: "*", Pattern: "/v1/board", Since: 1},
		{Method: "*", Pattern: "/v1/cloud/status", Since: 2},
	}}
	good := agentRouteFile{Routes: []agentRoute{
		{Method: "*", Pattern: "/v1/board", Guide: "board"},
		{Method: "*", Pattern: "/v1/cloud/status", Guide: "cloud"},
	}}
	if err := validateAgentRoutes(routes, good); err != nil {
		t.Fatal(err)
	}
	for name, mutation := range map[string]func(*agentRouteFile){
		"missing":      func(c *agentRouteFile) { c.Routes = c.Routes[:1] },
		"removed":      func(c *agentRouteFile) { c.Routes[1].Pattern = "/v1/cloud/old" },
		"duplicate":    func(c *agentRouteFile) { c.Routes = append(c.Routes, c.Routes[0]) },
		"unknown part": func(c *agentRouteFile) { c.Routes[0].Guide = "made-up" },
	} {
		t.Run(name, func(t *testing.T) {
			bad := agentRouteFile{Routes: append([]agentRoute(nil), good.Routes...)}
			mutation(&bad)
			if err := validateAgentRoutes(routes, bad); err == nil {
				t.Fatal("catalog drift was accepted")
			}
		})
	}
	for _, chinese := range []bool{false, true} {
		body := string(renderAgentRoutes(routes, good, chinese))
		for _, want := range []string{"API", "3", "/v1/board", "/v1/cloud/status", "clawdline guide board", "docs/user/board.md"} {
			if !strings.Contains(body, want) {
				t.Errorf("Chinese=%v: missing %q", chinese, want)
			}
		}
	}
}
