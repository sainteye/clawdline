package main

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"text/tabwriter"
	"time"

	"github.com/sainteye/clawdline/internal/adapters/projects"
)

// `clawdline project` asks the running daemon to change the explicit
// session-start registry. Its successful answer is from the same authority
// that serves the Project and start lists.
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
	case "devstack":
		fs := flag.NewFlagSet("project devstack", flag.ExitOnError)
		asJSON := fs.Bool("json", false, cliCopy("misc", "project.devstack_flag_json", "print the matching stacks as JSON"))
		_ = fs.Parse(args[1:])
		dir := "."
		if fs.NArg() > 0 {
			dir = fs.Arg(0)
		}
		b, err := openBroker(0)
		if err != nil {
			fail(err)
		}
		os.Exit(projectDevStack(os.Stdout, os.Stderr, b, dir, *asJSON))
	case "refresh":
		if err := runProjectRefresh(args[1:], os.Stdout); err != nil {
			if errors.Is(err, errProjectNotListed) {
				fmt.Fprintln(os.Stderr, "clawdline:", err)
				os.Exit(3)
			}
			fail(err)
		}
		return
	}
	switch args[0] {
	case "add":
		fs := flag.NewFlagSet("project add", flag.ExitOnError)
		asJSON := fs.Bool("json", false, "print the registry as JSON")
		_ = fs.Parse(args[1:])
		rows, err := projectRegistryRequest(http.MethodPost, fs.Args())
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
		rows, err := projectRegistryRequest(http.MethodDelete, fs.Args())
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
		rows, err := projectRegistryRequest(http.MethodGet, nil)
		if err != nil {
			fail(err)
		}
		printProjectRegistry(rows, *asJSON)
	default:
		projectUsage()
		os.Exit(2)
	}
}

func projectRegistryRequest(method string, paths []string) ([]projects.RegisteredPlace, error) {
	b, err := openBroker(0)
	if err != nil {
		return nil, err
	}
	path := "/v1/places"
	var body any
	if method == http.MethodGet {
		path += "?registered=1"
	} else {
		if method == http.MethodPost && len(paths) == 0 {
			paths = []string{"."}
		}
		absolute := make([]string, 0, len(paths))
		for _, p := range paths {
			p, err = filepath.Abs(p)
			if err != nil {
				return nil, err
			}
			absolute = append(absolute, p)
		}
		body = struct {
			Paths []string `json:"paths"`
		}{absolute}
	}
	a, err := b.request(method, path, nil, body, "")
	if err != nil {
		return nil, err
	}
	if !a.ok() {
		code, message := a.refusal()
		return nil, fmt.Errorf("%s: %s (%d)", code, message, a.Status)
	}
	var result struct {
		Registered bool                       `json:"registered"`
		Places     []projects.RegisteredPlace `json:"places"`
	}
	if err := json.Unmarshal(a.Body, &result); err != nil || !result.Registered || result.Places == nil {
		return nil, errors.New(cliCopy("misc", "project.daemon_answer_invalid", "the daemon did not return a Project list; update the running daemon"))
	}
	return result.Places, nil
}

func projectUsage() {
	fmt.Fprintln(os.Stderr, cliCopy("misc", "project.usage_clawdline_project_add_remove.7aad3274", "usage: clawdline project <add|remove|list> [--json] [directory…]"))
	fmt.Fprintln(os.Stderr, cliCopy("misc", "project.clawdline_project_export_out_file_n.08105c5e", "       clawdline project export [--out file] [--name source]"))
	fmt.Fprintln(os.Stderr, cliCopy("misc", "project.clawdline_project_import_clone_repl.fcdc6204", "       clawdline project import [--clone] [--replace-source] <file>"))
	fmt.Fprintln(os.Stderr, cliCopy("misc", "project.clawdline_project_unify_apply_check.4c4400ff", "       clawdline project unify [--apply | --check] [--json] [directory]"))
	fmt.Fprintln(os.Stderr, cliCopy("misc", "project.refresh_usage", "       clawdline project refresh [directory]"))
	fmt.Fprintln(os.Stderr, cliCopy("misc", "project.devstack_usage", "       clawdline project devstack [--json] [directory]"))
	fmt.Fprintln(os.Stderr, cliCopy("misc", "project.add_directory_keep_existing_directo.69166d26", "  add [directory…]      add directories; with none, add the current directory"))
	fmt.Fprintln(os.Stderr, cliCopy("misc", "project.remove_directory_forget_directories.79c5aae6", "  remove <directory…>   hide directories from Project lists without deleting them"))
	fmt.Fprintln(os.Stderr, cliCopy("misc", "project.list_show_explicitly_registered_dir.233ccc16", "  list                  show explicitly registered directories"))
	fmt.Fprintln(os.Stderr, cliCopy("misc", "project.export_this_machine_s_project_setti.62f59b09", "  export                this machine's project settings (icons, names, untracked skills), as a file"))
	fmt.Fprintln(os.Stderr, cliCopy("misc", "project.import_file_mirror_another_machine.f1f762b0", "  import <file>         mirror another machine's project settings here; it owns them from then on"))
	fmt.Fprintln(os.Stderr, cliCopy("misc", "project.unify_directory_show_how_claude_and.9b3b6da2", "  unify [directory]     show how Claude and Codex can share this Project's rules and skills; --apply does it"))
	fmt.Fprintln(os.Stderr, cliCopy("misc", "project.devstack_description", "  devstack [directory]  the servers Clawdline reads from this Project's .devstack.json, each port's state and its Link address"))
	fmt.Fprintln(os.Stderr, cliCopy("misc", "project.refresh_description", "  refresh [directory]   reread this Project's icon and display status"))
}

func printProjectRegistry(rows []projects.RegisteredPlace, asJSON bool) {
	sort.Slice(rows, func(i, j int) bool {
		if rows[i].AddedAt == rows[j].AddedAt {
			return rows[i].Path < rows[j].Path
		}
		return rows[i].AddedAt > rows[j].AddedAt
	})
	if asJSON {
		listed := make([]projectListRow, 0, len(rows))
		for _, row := range rows {
			listed = append(listed, projectListRow{ID: projects.PlaceID(row.Path), RegisteredPlace: row})
		}
		body, _ := json.MarshalIndent(map[string]any{"places": listed}, "", "  ")
		fmt.Println(string(body))
		return
	}
	w := tabwriter.NewWriter(os.Stdout, 0, 4, 2, ' ', 0)
	for _, row := range rows {
		fmt.Fprintf(w, "%s\t%s\t%s\n", time.Unix(row.AddedAt, 0).Format(time.RFC3339), projects.PlaceID(row.Path), row.Path)
	}
	_ = w.Flush()
}

// projectListRow is one registered place with the id Board routes accept for
// it. Without the id, `project_key` read as the value to pass, and a Session
// that passed it was refused three times before it found the id in SQLite.
type projectListRow struct {
	ID string `json:"id"`
	projects.RegisteredPlace
}

// printProjectCandidates follows a refused `item add` or `item propose`: the
// Projects a Board item may name, each with the id and directory either of
// which is accepted. It reads the Board's own catalog, which also holds
// recorded folders and live Sessions' directories that `project list` (the
// registered directories only) leaves out.
func printProjectCandidates(stderr io.Writer, b *broker) {
	a, err := b.request(http.MethodGet, "/v1/work/v2/agent/projects", nil, nil, "")
	if err != nil || !a.ok() {
		return
	}
	var result struct {
		Projects []struct {
			ID   string `json:"id"`
			Path string `json:"path"`
		} `json:"projects"`
	}
	if json.Unmarshal(a.Body, &result) != nil || len(result.Projects) == 0 {
		return
	}
	fmt.Fprintln(stderr, cliCopy("item", "label.project_candidates", "Projects on this machine (pass the id or the directory):"))
	w := tabwriter.NewWriter(stderr, 0, 4, 2, ' ', 0)
	for _, row := range result.Projects {
		fmt.Fprintf(w, "  %s\t%s\n", row.ID, row.Path)
	}
	_ = w.Flush()
}
