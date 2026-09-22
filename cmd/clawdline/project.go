package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"sort"
	"text/tabwriter"
	"time"

	"github.com/sainteye/clawdline/internal/adapters/projects"
	"github.com/sainteye/clawdline/internal/config"
	"github.com/sainteye/clawdline/internal/domain/capacity"
)

// `clawdline project` changes the explicit session-start registry. It writes
// this daemon's own state directly, so it works before the daemon starts and
// never needs an assistant call merely to remember a directory.
func projectCommand(args []string) {
	if len(args) == 0 {
		projectUsage()
		os.Exit(2)
	}
	registry := projects.OpenPlaceRegistry(config.Load().Dir, foreignDirs()...)
	resolved, _ := capacity.Resolve(capacity.Register(), os.Getenv(capacity.OverrideEnv))
	registry.SetLimit(capacity.Limit(resolved, capacity.PlacesRegistered))

	switch args[0] {
	case "add":
		fs := flag.NewFlagSet("project add", flag.ExitOnError)
		asJSON := fs.Bool("json", false, "print the registry as JSON")
		_ = fs.Parse(args[1:])
		if fs.NArg() == 0 {
			projectUsage()
			os.Exit(2)
		}
		rows, err := registry.Add(fs.Args(), time.Now())
		if err != nil {
			fail(err)
		}
		printProjectRegistry(rows, *asJSON)
	case "remove":
		fs := flag.NewFlagSet("project remove", flag.ExitOnError)
		asJSON := fs.Bool("json", false, "print the registry as JSON")
		_ = fs.Parse(args[1:])
		if fs.NArg() == 0 {
			projectUsage()
			os.Exit(2)
		}
		rows, err := registry.Remove(fs.Args())
		if err != nil {
			fail(err)
		}
		printProjectRegistry(rows, *asJSON)
	case "list":
		fs := flag.NewFlagSet("project list", flag.ExitOnError)
		asJSON := fs.Bool("json", false, "print the registry as JSON")
		_ = fs.Parse(args[1:])
		if fs.NArg() != 0 {
			fail(fmt.Errorf("unexpected argument %q", fs.Arg(0)))
		}
		rows, err := registry.List()
		if err != nil {
			fail(err)
		}
		printProjectRegistry(rows, *asJSON)
	default:
		projectUsage()
		os.Exit(2)
	}
}

func projectUsage() {
	fmt.Fprintln(os.Stderr, "usage: clawdline project <add|remove|list> [--json] [directory…]")
	fmt.Fprintln(os.Stderr, "  add <directory…>      keep existing directories in the session-start list")
	fmt.Fprintln(os.Stderr, "  remove <directory…>   forget directories without deleting them")
	fmt.Fprintln(os.Stderr, "  list                  show explicitly registered directories")
}

func printProjectRegistry(rows []projects.RegisteredPlace, asJSON bool) {
	sort.Slice(rows, func(i, j int) bool {
		if rows[i].AddedAt == rows[j].AddedAt {
			return rows[i].Path < rows[j].Path
		}
		return rows[i].AddedAt > rows[j].AddedAt
	})
	if asJSON {
		body, _ := json.MarshalIndent(map[string]any{"places": rows}, "", "  ")
		fmt.Println(string(body))
		return
	}
	w := tabwriter.NewWriter(os.Stdout, 0, 4, 2, ' ', 0)
	for _, row := range rows {
		fmt.Fprintf(w, "%s\t%s\n", time.Unix(row.AddedAt, 0).Format(time.RFC3339), row.Path)
	}
	_ = w.Flush()
}
