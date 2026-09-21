package orchestrator

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"sort"
	"strings"
	"unicode/utf8"
)

// Task graphs: a split-and-join declared in the tasks themselves
// (broker-design B8; the Swift app's OrchestratorPlanning.swift:95-199).
//
// A graph is not a table. Each task that is a node carries the whole graph in
// its brief, with `current_node` naming which node it is, and the graph's
// state is read from those tasks every time it is asked (D04): a node is
// active while its task runs, done when its task delivered — and, for a node
// whose work has to land, when the landing record says so with its proof
// (O10: the Swift app's landing node read only `state == landed`) — and
// ready or blocked by its dependencies. Nothing is stored that the next
// reading could not recompute.

// GraphKinds are the node kinds, the Swift app's.
var GraphKinds = []string{"decision", "delivery", "review", "correction", "verification", "landing"}

// Graph is `graph` in task.json.
type Graph struct {
	ID          string      `json:"id"`
	Destination string      `json:"destination"`
	CurrentNode string      `json:"current_node"`
	Nodes       []GraphNode `json:"nodes"`
	Unknowns    []string    `json:"unknowns,omitempty"`
	OutOfScope  []string    `json:"out_of_scope,omitempty"`
}

// GraphNode is one node.
type GraphNode struct {
	ID         string   `json:"id"`
	Title      string   `json:"title"`
	Kind       string   `json:"kind"`
	DependsOn  []string `json:"depends_on"`
	Acceptance []string `json:"acceptance"`
}

// The graph's limits, the Swift app's.
const (
	graphNodeLimit        = 32
	graphDestinationLimit = 500
	graphTitleLimit       = 120
	graphListLimit        = 8
	graphLineLimit        = 300
)

// Node states as a graph read reports them.
const (
	NodeReady           = "ready"
	NodeBlocked         = "blocked"
	NodeActive          = "active"
	NodeFailed          = "failed"
	NodeDone            = "done"
	NodeChangesRequired = "changes_required"
	NodeAwaitingLanding = "awaiting_landing"
)

func badGraph(message string) error {
	return refuse(http.StatusUnprocessableEntity, "bad_task", "graph: "+message)
}

// admitGraph reads and checks a brief's `graph`. Absent or null is no graph.
func admitGraph(raw json.RawMessage) (*Graph, error) {
	if len(raw) == 0 || string(raw) == "null" {
		return nil, nil
	}
	var g Graph
	dec := json.NewDecoder(strings.NewReader(string(raw)))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&g); err != nil {
		return nil, badGraph("must be an object with id, destination, current_node and nodes: " + err.Error())
	}
	if !uuidShape.MatchString(g.ID) {
		return nil, badGraph("id must be a lowercase UUID.")
	}
	if n := utf8.RuneCountInString(strings.TrimSpace(g.Destination)); n == 0 || n > graphDestinationLimit {
		return nil, badGraph("destination must be 1–500 characters.")
	}
	if len(g.Nodes) == 0 || len(g.Nodes) > graphNodeLimit {
		return nil, badGraph("nodes must hold 1–32 nodes.")
	}
	kinds := map[string]bool{}
	for _, k := range GraphKinds {
		kinds[k] = true
	}
	ids := map[string]bool{}
	for _, n := range g.Nodes {
		if !graphNodeID(n.ID) || ids[n.ID] {
			return nil, badGraph("every node needs a unique id of 1–64 of [a-z0-9._-].")
		}
		ids[n.ID] = true
		if t := utf8.RuneCountInString(strings.TrimSpace(n.Title)); t == 0 || t > graphTitleLimit {
			return nil, badGraph("node " + n.ID + ": title must be 1–120 characters.")
		}
		if !kinds[n.Kind] {
			return nil, badGraph("node " + n.ID + ": kind must be one of " + strings.Join(GraphKinds, ", ") + ".")
		}
		if len(n.Acceptance) == 0 || len(n.Acceptance) > graphListLimit || !lines(n.Acceptance) {
			return nil, badGraph("node " + n.ID + ": acceptance must be 1–8 lines of at most 300 characters.")
		}
	}
	for _, n := range g.Nodes {
		for _, d := range n.DependsOn {
			if d == n.ID || !ids[d] {
				return nil, badGraph("node " + n.ID + " depends on " + d + ", which is not another node of this graph.")
			}
		}
	}
	if cyclic(g.Nodes) {
		return nil, badGraph("the dependencies form a cycle.")
	}
	if !ids[g.CurrentNode] {
		return nil, badGraph("current_node must name one of its nodes.")
	}
	for _, list := range [][]string{g.Unknowns, g.OutOfScope} {
		if len(list) > graphListLimit || !lines(list) {
			return nil, badGraph("unknowns and out_of_scope are at most 8 lines of at most 300 characters.")
		}
	}
	return &g, nil
}

func graphNodeID(v string) bool {
	if v == "" || len(v) > 64 {
		return false
	}
	for _, r := range v {
		if !(r >= 'a' && r <= 'z' || r >= '0' && r <= '9' || r == '.' || r == '_' || r == '-') {
			return false
		}
	}
	return true
}

func lines(list []string) bool {
	for _, l := range list {
		if n := utf8.RuneCountInString(strings.TrimSpace(l)); n == 0 || n > graphLineLimit {
			return false
		}
	}
	return true
}

func cyclic(nodes []GraphNode) bool {
	deps := map[string][]string{}
	for _, n := range nodes {
		deps[n.ID] = n.DependsOn
	}
	const (
		white = iota
		grey
		black
	)
	color := map[string]int{}
	var visit func(string) bool
	visit = func(id string) bool {
		color[id] = grey
		for _, d := range deps[id] {
			switch color[d] {
			case grey:
				return true
			case white:
				if visit(d) {
					return true
				}
			}
		}
		color[id] = black
		return false
	}
	for _, n := range nodes {
		if color[n.ID] == white && visit(n.ID) {
			return true
		}
	}
	return false
}

// definition is what two tasks naming one graph must agree on: everything but
// which node each of them is.
func (g Graph) definition() string {
	g.CurrentNode = ""
	body, _ := json.Marshal(g)
	sum := sha256.Sum256(body)
	return hex.EncodeToString(sum[:])
}

func (g Graph) node(id string) (GraphNode, bool) {
	for _, n := range g.Nodes {
		if n.ID == id {
			return n, true
		}
	}
	return GraphNode{}, false
}

// nodeState is what one task says about the node it ran.
func nodeState(r Record, kind string) string {
	switch {
	case !r.State.Terminal():
		return NodeActive
	case r.State != StateSuccess:
		return NodeFailed
	}
	if kind == "review" && r.Result != nil && len(r.Result.Review) > 0 {
		var review struct {
			Verdict string `json:"verdict"`
		}
		if json.Unmarshal(r.Result.Review, &review) == nil && review.Verdict == "changes_required" {
			return NodeChangesRequired
		}
	}
	if r.Landing != nil {
		switch r.Landing.State {
		case LandingPending:
			return NodeAwaitingLanding
		case LandingLanded, LandingIncorporated:
			// O10: settled with durable landing evidence (D17), never the word
			// alone.
			if r.Landing.TargetCommit == "" && r.Landing.DeliveryHead == "" {
				return NodeAwaitingLanding
			}
		}
	}
	return NodeDone
}

// GraphNodeView is a node as a graph read reports it.
type GraphNodeView struct {
	GraphNode
	State string `json:"state"`
	Task  string `json:"task_id,omitempty"`
}

// GraphView is one graph as its tasks say it is now.
type GraphView struct {
	ID          string          `json:"id"`
	Destination string          `json:"destination"`
	Nodes       []GraphNodeView `json:"nodes"`
	// Frontier is the nodes that can be dispatched now.
	Frontier []string `json:"frontier"`
	// Conflicts is every task whose graph disagrees with the first one read.
	Conflicts []string `json:"conflicts,omitempty"`
}

// graphViews reads every graph the records name.
func graphViews(records []Record) []GraphView {
	type acc struct {
		g         Graph
		def       string
		latest    map[string]Record
		conflicts []string
	}
	graphs := map[string]*acc{}
	order := []string{}
	// Oldest first, so the definition that stands is the first one written.
	sort.SliceStable(records, func(i, j int) bool { return records[i].CreatedAt.Before(records[j].CreatedAt) })
	for _, r := range records {
		if r.Graph == nil {
			continue
		}
		a, ok := graphs[r.Graph.ID]
		if !ok {
			a = &acc{g: *r.Graph, def: r.Graph.definition(), latest: map[string]Record{}}
			graphs[r.Graph.ID] = a
			order = append(order, r.Graph.ID)
		}
		if r.Graph.definition() != a.def {
			a.conflicts = append(a.conflicts, r.ID)
			continue
		}
		a.latest[r.Graph.CurrentNode] = r
	}
	out := []GraphView{}
	for _, id := range order {
		a := graphs[id]
		states := map[string]string{}
		for _, n := range a.g.Nodes {
			if r, ok := a.latest[n.ID]; ok {
				states[n.ID] = nodeState(r, n.Kind)
			}
		}
		view := GraphView{ID: id, Destination: a.g.Destination, Nodes: []GraphNodeView{}, Frontier: []string{},
			Conflicts: a.conflicts}
		for _, n := range a.g.Nodes {
			v := GraphNodeView{GraphNode: n, State: states[n.ID]}
			if r, ok := a.latest[n.ID]; ok {
				v.Task = r.ID
			}
			if v.State == "" || v.State == NodeFailed || v.State == NodeChangesRequired {
				if len(blockers(a.g, n, states)) == 0 {
					if v.State == "" {
						v.State = NodeReady
					}
					view.Frontier = append(view.Frontier, n.ID)
				} else if v.State == "" {
					v.State = NodeBlocked
				}
			}
			view.Nodes = append(view.Nodes, v)
		}
		out = append(out, view)
	}
	return out
}

// blockers is every dependency of n that is not done.
func blockers(g Graph, n GraphNode, states map[string]string) []map[string]any {
	out := []map[string]any{}
	for _, d := range n.DependsOn {
		if states[d] != NodeDone {
			state := states[d]
			if state == "" {
				state = NodeReady
			}
			out = append(out, map[string]any{"node_id": d, "state": state})
		}
	}
	return out
}

// checkGraph refuses a dispatch its graph cannot take now: a definition that
// disagrees with the graph's other tasks, a node already running or already
// done, or a node whose dependencies are not done.
func (b *Broker) checkGraph(ctx context.Context, r Record) error {
	if r.Graph == nil {
		return nil
	}
	records, err := b.records(ctx)
	if err != nil {
		return err
	}
	n, _ := r.Graph.node(r.Graph.CurrentNode)
	var views []GraphView
	for _, v := range graphViews(records) {
		if v.ID == r.Graph.ID {
			views = append(views, v)
		}
	}
	if len(views) == 0 {
		// The graph's first task: only its dependencies can stop it, and a
		// graph nobody has started has none done.
		if len(n.DependsOn) > 0 {
			return b.frontierRefusal(r, blockers(*r.Graph, n, map[string]string{}))
		}
		return nil
	}
	for _, other := range records {
		if other.Graph != nil && other.Graph.ID == r.Graph.ID && other.Graph.definition() != r.Graph.definition() {
			return refuseWith(http.StatusConflict, "graph_definition_conflict",
				"This graph id is already defined differently by another task; a graph has one definition.",
				map[string]any{"graph_id": r.Graph.ID, "task_id": other.ID})
		}
	}
	states := map[string]string{}
	for _, node := range views[0].Nodes {
		states[node.ID] = node.State
		if node.ID != n.ID {
			continue
		}
		switch node.State {
		case NodeActive:
			return refuseWith(http.StatusConflict, "graph_node_active",
				"A task for this node is still running.", map[string]any{"graph_id": r.Graph.ID, "node_id": n.ID, "task_id": node.Task})
		case NodeDone, NodeAwaitingLanding:
			return refuseWith(http.StatusConflict, "graph_node_complete",
				"This node's task already delivered.", map[string]any{"graph_id": r.Graph.ID, "node_id": n.ID, "task_id": node.Task})
		}
	}
	if blocking := blockers(*r.Graph, n, states); len(blocking) > 0 {
		return b.frontierRefusal(r, blocking)
	}
	return nil
}

func (b *Broker) frontierRefusal(r Record, blocking []map[string]any) error {
	code := "graph_frontier_blocked"
	for _, x := range blocking {
		if x["state"] == NodeFailed {
			code = "graph_dependency_failed"
		}
	}
	return refuseWith(http.StatusConflict, code,
		"This node depends on nodes that are not done; dispatch them first.",
		map[string]any{"graph_id": r.Graph.ID, "node_id": r.Graph.CurrentNode, "blocking_nodes": blocking})
}

// Graphs is every graph the broker's tasks name, as they say it is now.
func (b *Broker) Graphs(ctx context.Context) ([]GraphView, error) {
	records, err := b.records(ctx)
	if err != nil {
		return nil, err
	}
	return graphViews(records), nil
}
