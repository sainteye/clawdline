package main

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"

	"github.com/sainteye/clawdline/internal/domain/toollibrary"
)

func toolboxCommand(args []string) {
	if len(args) == 0 || args[0] != "search" {
		fmt.Fprintln(os.Stderr, cliCopy("entry", "toolbox_usage", "usage: clawdline toolbox search [terms]"))
		os.Exit(2)
	}
	b, err := openBroker(0)
	if err != nil {
		fail(err)
	}
	os.Exit(runToolboxSearch(b, strings.Join(args[1:], " "), os.Stdout, os.Stderr))
}

// Search reads only this daemon's metadata. It never fetches upstream sources,
// installs a tool, changes a setting or invokes a model.
func runToolboxSearch(b *broker, query string, out, errOut io.Writer) int {
	answer, err := b.request(http.MethodGet, "/v1/tool-library", nil, nil, "")
	if err != nil {
		fmt.Fprintln(errOut, err)
		return 1
	}
	if answer.Status != http.StatusOK {
		return report(out, errOut, "toolbox search", answer)
	}
	var library struct {
		Saved   []toollibrary.Project `json:"saved"`
		Catalog toollibrary.Catalog   `json:"catalog"`
	}
	if err := json.Unmarshal(answer.Body, &library); err != nil || library.Saved == nil || library.Catalog.Projects == nil {
		fmt.Fprintln(errOut, "tool_library_invalid_reply")
		return 1
	}
	type match struct {
		ID      string              `json:"id"`
		RepoURL string              `json:"repo_url"`
		Name    toollibrary.Names   `json:"name"`
		Summary toollibrary.Names   `json:"summary"`
		Commit  string              `json:"source_commit"`
		Kind    string              `json:"integration_kind,omitempty"`
		Skills  []toollibrary.Skill `json:"skills"`
	}
	matches := []match{}
	for _, p := range toollibrary.Search(library.Catalog, library.Saved, query) {
		matches = append(matches, match{p.ID, p.RepoURL, p.Name, p.Summary, p.Commit, p.IntegrationKind, p.Skills})
	}
	if err := json.NewEncoder(out).Encode(struct {
		Query   string  `json:"query"`
		Matches []match `json:"matches"`
	}{query, matches}); err != nil {
		fmt.Fprintln(errOut, err)
		return 1
	}
	return 0
}
