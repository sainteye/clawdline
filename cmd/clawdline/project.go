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
	switch args[0] {
	case "export":
		projectExport(args[1:])
		return
	case "import":
		projectImport(args[1:])
		return
	case "unify":
		projectUnifyCommand(args[1:])
		return
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
			fail(fmt.Errorf(cliCopy("misc", "project.unexpected_argument", "unexpected argument %q"), fs.Arg(0)))
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
	fmt.Fprintln(os.Stderr, cliCopy("misc", "project.usage_clawdline_project_add_remove.7aad3274", "usage: clawdline project <add|remove|list> [--json] [directory…]"))
	fmt.Fprintln(os.Stderr, cliCopy("misc", "project.clawdline_project_export_out_file_n.08105c5e", "       clawdline project export [--out file] [--name source]"))
	fmt.Fprintln(os.Stderr, cliCopy("misc", "project.clawdline_project_import_clone_repl.fcdc6204", "       clawdline project import [--clone] [--replace-source] <file>"))
	fmt.Fprintln(os.Stderr, cliCopy("misc", "project.clawdline_project_unify_apply_check.4c4400ff", "       clawdline project unify [--apply | --check] [--json] [directory]"))
	fmt.Fprintln(os.Stderr, cliCopy("misc", "project.add_directory_keep_existing_directo.69166d26", "  add <directory…>      keep existing directories in the session-start list"))
	fmt.Fprintln(os.Stderr, cliCopy("misc", "project.remove_directory_forget_directories.79c5aae6", "  remove <directory…>   forget directories without deleting them"))
	fmt.Fprintln(os.Stderr, cliCopy("misc", "project.list_show_explicitly_registered_dir.233ccc16", "  list                  show explicitly registered directories"))
	fmt.Fprintln(os.Stderr, cliCopy("misc", "project.export_this_machine_s_project_setti.62f59b09", "  export                this machine's project settings (icons, names, untracked skills), as a file"))
	fmt.Fprintln(os.Stderr, cliCopy("misc", "project.import_file_mirror_another_machine.f1f762b0", "  import <file>         mirror another machine's project settings here; it owns them from then on"))
	fmt.Fprintln(os.Stderr, cliCopy("misc", "project.unify_directory_show_how_claude_and.9b3b6da2", "  unify [directory]     show how Claude and Codex can share this Project's rules and skills; --apply does it"))
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
