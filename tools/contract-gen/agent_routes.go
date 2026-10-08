package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"sort"
)

// agentRoute owns only the route's documentation destination. The route
// pattern, API level, and introduction level still come from httptransport.
type agentRoute struct {
	Method  string `json:"method"`
	Pattern string `json:"pattern"`
	Guide   string `json:"guide"`
}

type agentRouteFile struct {
	Description string       `json:"description"`
	Routes      []agentRoute `json:"routes"`
}

var agentRouteGoals = map[string]string{
	"connect": "docs/user/first-session.md", "send": "docs/user/sessions.md",
	"note": "docs/user/sessions.md", "notify": "docs/user/notifications.md",
	"cloud": "docs/user/cloud-first-look.md", "project": "docs/user/projects.md",
	"board": "docs/user/board.md", "feature-root": "docs/user/board.md",
	"epic": "docs/user/board.md", "dispatch": "docs/user/clawdfather-and-dispatch.md",
	"inventory":    "docs/user/clawdfather-and-dispatch.md",
	"running":      "docs/user/clawdfather-and-dispatch.md",
	"callback":     "docs/user/clawdfather-and-dispatch.md",
	"landing":      "docs/user/clawdfather-and-dispatch.md",
	"coordination": "docs/user/clawdfather-and-dispatch.md",
	"schedule":     "docs/user/schedules.md", "report": "docs/user/sessions.md",
}

// validateAgentRoutes makes a new, removed, or reclassified route an explicit
// review of the Agent reference. A prefix match would silently bless a new
// sub-route without anyone deciding which guide part owns its semantics.
func validateAgentRoutes(routes routesFile, catalog agentRouteFile) error {
	known := map[string]routeRow{}
	for _, r := range routes.Routes {
		known[r.Method+" "+r.Pattern] = r
	}
	seen := map[string]bool{}
	for _, r := range catalog.Routes {
		key := r.Method + " " + r.Pattern
		if seen[key] {
			return fmt.Errorf("agent-routes.json repeats %s", key)
		}
		seen[key] = true
		if _, ok := known[key]; !ok {
			return fmt.Errorf("agent-routes.json names unregistered %s", key)
		}
		if _, ok := agentRouteGoals[r.Guide]; !ok {
			return fmt.Errorf("agent-routes.json names unknown guide part %q for %s", r.Guide, key)
		}
	}
	for key := range known {
		if !seen[key] {
			return fmt.Errorf("agent-routes.json is missing registered %s", key)
		}
	}
	return nil
}

func readAgentRoutes(path string) (agentRouteFile, error) {
	var out agentRouteFile
	body, err := os.ReadFile(path)
	if err != nil {
		return out, err
	}
	if err := json.Unmarshal(body, &out); err != nil {
		return out, err
	}
	return out, nil
}

func renderAgentRoutes(routes routesFile, catalog agentRouteFile, traditionalChinese bool) []byte {
	byRoute := map[string]agentRoute{}
	for _, r := range catalog.Routes {
		byRoute[r.Method+" "+r.Pattern] = r
	}
	var b bytes.Buffer
	if traditionalChinese {
		fmt.Fprintf(&b, "# 此 build 的 Agent 路由索引\n\n由已註冊的路由與 `api/v1/agent-routes.json` 產生；API 等級：%d。請先讀 `clawdline guide`，再讀表格指定的部分。路由出現在此只表示可以探索，**不授權呼叫，也不代表操作完成**。每個前綴下的具體動作、輸入、權限、拒絕與收據，必須由該部分及本 build 的 schema 明示；若未載明該動作，Agent 就不得呼叫。缺少能力或版本、身分不符、結果未知時停止，不改走另一台機器或舊介面。\n\n", routes.APILevel)
		b.WriteString("| 方法 | 已註冊路由 | 起始 API 等級 | Agent 指南 | 使用者目標 |\n| --- | --- | ---: | --- | --- |\n")
	} else {
		fmt.Fprintf(&b, "# Agent routes in this build\n\nGenerated from the registered routes and `api/v1/agent-routes.json`; API level: %d. Read `clawdline guide` first, then the part named below. A route here is discoverability, **not authorization or proof of completion**. For an action under a prefix, its input, permission, refusals, and receipt must be stated by that part and this build's schema. If the part does not specify that action, treat the route as unavailable to an Agent. Stop on a missing capability or version, mismatched identity, or unknown outcome; do not switch machines or use an older interface.\n\n", routes.APILevel)
		b.WriteString("| Method | Registered route | Since API level | Agent guide | Human goal |\n| --- | --- | ---: | --- | --- |\n")
	}
	rows := append([]routeRow(nil), routes.Routes...)
	sort.Slice(rows, func(i, j int) bool {
		if rows[i].Pattern == rows[j].Pattern {
			return rows[i].Method < rows[j].Method
		}
		return rows[i].Pattern < rows[j].Pattern
	})
	for _, r := range rows {
		meta := byRoute[r.Method+" "+r.Pattern]
		fmt.Fprintf(&b, "| `%s` | `%s` | %d | `clawdline guide %s` | `%s` |\n", r.Method, r.Pattern, r.Since, meta.Guide, agentRouteGoals[meta.Guide])
	}
	return b.Bytes()
}
