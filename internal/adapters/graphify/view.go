package graphify

import (
	"context"
	"encoding/json"
	"sort"
	"strconv"
	"time"
)

type ViewNode struct {
	ID        string `json:"id"`
	Label     string `json:"label"`
	File      string `json:"file"`
	Location  string `json:"location"`
	Community string `json:"community"`
}
type ViewLink struct {
	Source     string `json:"source"`
	Target     string `json:"target"`
	Relation   string `json:"relation"`
	Confidence string `json:"confidence"`
}
type ViewData struct {
	UnverifiedSources int64       `json:"unverified_sources"`
	Build             BuildStatus `json:"build"`
	CanBuild          bool        `json:"can_build"`
	State             string      `json:"state"`
	Reason            string      `json:"reason"`
	CheckedAt         int64       `json:"checked_at"`
	TotalNodes        int64       `json:"total_nodes"`
	TotalLinks        int64       `json:"total_links"`
	Truncated         bool        `json:"truncated"`
	Nodes             []ViewNode  `json:"nodes"`
	Links             []ViewLink  `json:"links"`
}

// View bounds cap a single on-demand read, not a retained graph cache.
const (
	ViewNodeLimit = 300
	ViewLinkLimit = 900
	ViewTextLimit = 120
)

// View exposes only inert, bounded text and numeric identifiers. Native HTML,
// source text, URLs, arbitrary graph attributes and scripts never leave here.
func View(ctx context.Context, root string) (ViewData, error) {
	var raw []byte
	status, err := inspect(ctx, root, &raw)
	out := ViewData{State: status.State, Reason: status.Reason, CheckedAt: time.Now().Unix(), Nodes: []ViewNode{}, Links: []ViewLink{}}
	if err != nil {
		return out, err
	}
	if status.State != Matched && status.State != "stale" {
		return out, nil
	}
	var graph struct {
		Graph struct {
			UnverifiedSources int64 `json:"unverified_sources"`
		} `json:"graph"`
		Nodes []struct {
			ID        string          `json:"id"`
			Label     string          `json:"label"`
			File      string          `json:"source_file"`
			Location  string          `json:"source_location"`
			Community json.RawMessage `json:"community"`
		} `json:"nodes"`
		Links []struct {
			Source     string `json:"source"`
			Target     string `json:"target"`
			Relation   string `json:"relation"`
			Confidence string `json:"confidence"`
		} `json:"links"`
	}
	if json.Unmarshal(raw, &graph) != nil {
		out.State = "unverified"
		out.Reason = "graphify_graph_format_unverified"
		return out, nil
	}
	out.UnverifiedSources = graph.Graph.UnverifiedSources
	if out.UnverifiedSources < 0 || out.UnverifiedSources > int64(len(graph.Nodes)) {
		out.State = "unverified"
		out.Reason = "graphify_graph_format_unverified"
		return out, nil
	}
	out.TotalNodes = int64(len(graph.Nodes))
	out.TotalLinks = int64(len(graph.Links))
	degrees := map[string]int{}
	known := map[string]bool{}
	for _, n := range graph.Nodes {
		if n.ID == "" || known[n.ID] {
			out.State = "unverified"
			out.Reason = "graphify_graph_format_unverified"
			return out, nil
		}
		known[n.ID] = true
	}
	for _, l := range graph.Links {
		if !known[l.Source] || !known[l.Target] {
			out.State = "unverified"
			out.Reason = "graphify_graph_format_unverified"
			return out, nil
		}
		degrees[l.Source]++
		degrees[l.Target]++
	}
	// A large graph has an explicitly labelled overview of its most connected nodes.
	sort.SliceStable(graph.Nodes, func(i, j int) bool { return degrees[graph.Nodes[i].ID] > degrees[graph.Nodes[j].ID] })
	ids := map[string]string{}
	for i, n := range graph.Nodes {
		if i >= ViewNodeLimit {
			break
		}
		id := strconv.Itoa(i)
		ids[n.ID] = id
		label := n.Label
		if label == "" {
			label = n.ID
		}
		community := ""
		if len(n.Community) > 0 {
			var v any
			if json.Unmarshal(n.Community, &v) == nil {
				switch x := v.(type) {
				case string:
					community = x
				case float64:
					community = strconv.FormatFloat(x, 'f', -1, 64)
				}
			}
		}
		out.Nodes = append(out.Nodes, ViewNode{ID: id, Label: viewText(label), File: viewText(n.File), Location: viewText(n.Location), Community: viewText(community)})
	}
	for _, l := range graph.Links {
		a, aok := ids[l.Source]
		b, bok := ids[l.Target]
		if !aok || !bok || len(out.Links) >= ViewLinkLimit {
			continue
		}
		confidence := "UNKNOWN"
		if l.Confidence == "EXTRACTED" || l.Confidence == "INFERRED" {
			confidence = l.Confidence
		}
		out.Links = append(out.Links, ViewLink{Source: a, Target: b, Relation: viewText(l.Relation), Confidence: confidence})
	}
	out.Truncated = len(out.Nodes) < len(graph.Nodes) || len(out.Links) < len(graph.Links)
	return out, nil
}

func viewText(s string) string {
	r := []rune(s)
	if len(r) > ViewTextLimit {
		return string(r[:ViewTextLimit-1]) + "…"
	}
	return s
}
