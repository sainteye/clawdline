package main

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"time"

	psync "github.com/sainteye/clawdline/internal/adapters/projectsync"
	domain "github.com/sainteye/clawdline/internal/domain/projectsync"
)

// projectBundle is `clawdline project export`'s file: every offered project
// with its contents, and the name of the machine that owns them. It is the
// same data a Cloud viewer carries (docs/project-sync.md), for a person who
// moves it some other way.
type projectBundle struct {
	Version  int            `json:"version"`
	Source   domain.Source  `json:"source"`
	At       int64          `json:"at"`
	Projects []domain.Entry `json:"projects"`
}

func daemonJSON(method, path string, body, into any) error {
	req, err := daemonRequest(method, path, body)
	if err != nil {
		return err
	}
	client := &http.Client{Timeout: 2 * time.Minute}
	res, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("the daemon did not answer: %w", err)
	}
	defer res.Body.Close()
	if res.StatusCode/100 != 2 {
		return errors.New(refusalText(res))
	}
	return json.NewDecoder(io.LimitReader(res.Body, 64<<20)).Decode(into)
}

func projectExport(args []string) {
	fs := flag.NewFlagSet("project export", flag.ExitOnError)
	out := fs.String("out", "", "write the bundle here instead of standard output")
	name := fs.String("name", "", "the source machine's name in the bundle (default: host name)")
	_ = fs.Parse(args)
	var manifest domain.Manifest
	if err := daemonJSON("GET", "/v1/project-sync/manifest", nil, &manifest); err != nil {
		fail(err)
	}
	source := *name
	if source == "" {
		source, _ = os.Hostname()
	}
	bundle := projectBundle{Version: domain.Version, Source: domain.Source{Name: source}, At: time.Now().Unix(), Projects: []domain.Entry{}}
	for _, p := range manifest.Projects {
		var answer struct {
			Project domain.Entry `json:"project"`
		}
		if err := daemonJSON("GET", "/v1/project-sync/entry?repo="+url.QueryEscape(p.Repo), nil, &answer); err != nil {
			fmt.Fprintf(os.Stderr, "clawdline: %s: %v\n", p.Repo, err)
			continue
		}
		bundle.Projects = append(bundle.Projects, answer.Project)
	}
	for _, s := range manifest.Skipped {
		fmt.Fprintf(os.Stderr, "skipped %s (%s): %s\n", s.Label, s.Path, s.Reason)
	}
	data, err := json.MarshalIndent(bundle, "", "  ")
	if err != nil {
		fail(err)
	}
	if *out == "" {
		fmt.Println(string(data))
		return
	}
	if err := os.WriteFile(*out, append(data, '\n'), 0o600); err != nil {
		fail(err)
	}
	fmt.Fprintf(os.Stderr, "exported %d project(s) to %s\n", len(bundle.Projects), *out)
}

func projectImport(args []string) {
	fs := flag.NewFlagSet("project import", flag.ExitOnError)
	clone := fs.Bool("clone", false, "clone a repository this machine does not have yet")
	replace := fs.Bool("replace-source", false, "take over projects another source already mirrors here")
	_ = fs.Parse(args)
	if fs.NArg() != 1 {
		projectUsage()
		os.Exit(2)
	}
	data, err := os.ReadFile(fs.Arg(0))
	if err != nil {
		fail(err)
	}
	var bundle projectBundle
	if err := json.Unmarshal(data, &bundle); err != nil {
		fail(fmt.Errorf("not a project bundle: %w", err))
	}
	if bundle.Version != domain.Version {
		fail(fmt.Errorf("the bundle has version %d; this build reads %d", bundle.Version, domain.Version))
	}
	failed := 0
	for _, p := range bundle.Projects {
		var answer struct {
			Result psync.ApplyResult `json:"result"`
		}
		req := psync.ApplyRequest{Source: bundle.Source, Project: p, Clone: *clone, ReplaceSource: *replace}
		if err := daemonJSON("POST", "/v1/project-sync/mirror", req, &answer); err != nil {
			fmt.Fprintf(os.Stderr, "%s: %v\n", p.Repo, err)
			failed++
			continue
		}
		r := answer.Result
		fmt.Printf("%-12s %s %s  written %d, deleted %d, kept %d\n", r.State, r.Repo, r.Path, len(r.Written), len(r.Deleted), len(r.Kept))
		for _, k := range r.Kept {
			fmt.Printf("             kept %s (%s)\n", k.Path, k.Reason)
		}
	}
	if failed > 0 {
		os.Exit(1)
	}
}
