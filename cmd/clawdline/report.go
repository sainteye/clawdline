package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/sainteye/clawdline/internal/adapters/turnreport"
	"github.com/sainteye/clawdline/internal/config"
)

// `clawdline report`: the status report a Session leaves at the end of a turn,
// as one HTML file on this machine, and its file:// address on the last line.
// No daemon, no network: the commits are read from git, one at a time, and
// the page loads nothing (internal/adapters/turnreport).

func reportCommand(args []string) {
	os.Exit(runReport(os.Stdout, os.Stderr, args, time.Now(), os.Getenv))
}

func reportUsage(stderr io.Writer) int {
	fmt.Fprintln(stderr, cliCopy("workflow", "report.usage_clawdline_report_status_file_repo_dir_notes", "usage: clawdline report --status <file> [--repo dir] [--notes file] [--exclude path]… [--pin path]… [--at rev] [--lang en|zh-TW] [--out file|dir] [--open] <commit>…"))
	fmt.Fprintln(stderr, cliCopy("workflow", "report.commits_are_this_turn_s_own_oldest_first", "  commits are this turn's own, oldest first; each is read on its own, so other Sessions' commits between them stay out"))
	fmt.Fprintln(stderr, cliCopy("workflow", "report.status_a_markdown_file_optional_title_then_one", "  --status: a Markdown file, optional \"# title\", then one \"## \" heading and text per card"))
	fmt.Fprintln(stderr, cliCopy("workflow", "report.notes_one_path_sentence_per_line_shown_above", "  --notes: one `path: sentence` per line, shown above that file"))
	fmt.Fprintln(stderr, cliCopy("workflow", "report.out_default_state_dir_reports_date_id_report", "  --out: default <state dir>/reports/<date>-<id>/report.html, outside every repository, which this machine's daemon also answers at http://127.0.0.1"))
	fmt.Fprintln(stderr, cliCopy("workflow", "report.prints_the_file_address_then_for_a_report", "  prints the file:// address, then, for a report kept in the state directory, the daemon's http:// address"))
	return 2
}

func runReport(stdout, stderr io.Writer, args []string, now time.Time, getenv func(string) string) int {
	fs := flag.NewFlagSet("report", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	statusFile := fs.String("status", "", "")
	repo := fs.String("repo", ".", "")
	notesFile := fs.String("notes", "", "")
	at := fs.String("at", "", "")
	lang := fs.String("lang", "", "")
	out := fs.String("out", "", "")
	openIt := fs.Bool("open", false, "")
	var exclude, pins stringList
	fs.Var(&exclude, "exclude", "")
	fs.Var(&pins, "pin", "")
	if err := fs.Parse(args); err != nil || *statusFile == "" || fs.NArg() == 0 {
		return reportUsage(stderr)
	}
	switch *lang {
	case "":
		*lang = localeLanguage(getenv)
	case "en", "zh-TW":
	default:
		return reportUsage(stderr)
	}
	raw, err := os.ReadFile(*statusFile)
	if err != nil {
		fmt.Fprintln(stderr, cliCopy("workflow", "report.clawdline_report", "clawdline report:"), err)
		return 1
	}
	status, err := turnreport.ParseStatus(string(raw))
	if err != nil {
		fmt.Fprintf(stderr, cliCopy("workflow", "report.clawdline_report_s_v", "clawdline report: %s: %v\n"), *statusFile, err)
		return 1
	}
	notes := map[string]string{}
	if *notesFile != "" {
		raw, err := os.ReadFile(*notesFile)
		if err != nil {
			fmt.Fprintln(stderr, cliCopy("workflow", "report.clawdline_report", "clawdline report:"), err)
			return 1
		}
		if notes, err = turnreport.ParseNotes(string(raw)); err != nil {
			fmt.Fprintf(stderr, cliCopy("workflow", "report.clawdline_report_s_v", "clawdline report: %s: %v\n"), *notesFile, err)
			return 1
		}
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	c, err := turnreport.Collect(ctx, turnreport.Options{
		Repo: *repo, Commits: fs.Args(), At: *at, Exclude: exclude, Notes: notes, Pins: pins,
	})
	if err != nil {
		fmt.Fprintln(stderr, cliCopy("workflow", "report.clawdline_report", "clawdline report:"), err)
		return 1
	}
	project := filepath.Base(c.Root)
	date := now.Format("2006-01-02")
	page, err := turnreport.Render(c, status, *lang, date, project)
	if err != nil {
		fmt.Fprintln(stderr, cliCopy("workflow", "report.clawdline_report", "clawdline report:"), err)
		return 1
	}
	dest, id, err := reportPath(*out, config.Dir(), date, project)
	if err != nil {
		fmt.Fprintln(stderr, cliCopy("workflow", "report.clawdline_report", "clawdline report:"), err)
		return 1
	}
	if err := writeReport(dest, page); err != nil {
		fmt.Fprintln(stderr, cliCopy("workflow", "report.clawdline_report", "clawdline report:"), err)
		return 1
	}
	for p := range notes {
		if !c.Has(p) {
			fmt.Fprintf(stderr, cliCopy("workflow", "report.clawdline_report_a_note_names_s_which_this", "clawdline report: a note names %s, which this turn's commits do not touch\n"), p)
		}
	}
	for _, o := range c.Omitted {
		fmt.Fprintf(stderr, cliCopy("workflow", "report.clawdline_report_not_listed_or_shortened_s_s", "clawdline report: not listed or shortened: %s (%s)\n"), o.Path, o.Reason)
	}
	if inside(c.Root, dest) {
		fmt.Fprintf(stderr, cliCopy("workflow", "report.clawdline_report_s_is_inside_s_keep_it", "clawdline report: %s is inside %s; keep it out of a commit\n"), dest, c.Root)
	}
	fmt.Fprintf(stderr, cliCopy("workflow", "report.clawdline_report_d_files_from_d_commits_1f", "clawdline report: %d files from %d commits, %.1f MB\n"), len(c.Files), len(c.Order), float64(len(page))/1e6)
	link := fileURL(dest)
	if *openIt {
		if err := openURL(link); err != nil {
			fmt.Fprintf(stderr, cliCopy("workflow", "report.clawdline_report_could_not_open_it_v_open", "clawdline report: could not open it (%v); open the address below\n"), err)
		}
	}
	fmt.Fprintln(stdout, link)
	if id == "" {
		fmt.Fprintln(stderr, cliCopy("workflow", "report.clawdline_report_written_outside_the_state_directory_so", "clawdline report: written outside the state directory, so the daemon does not answer it; there is no http address"))
		return 0
	}
	port, err := daemonPort()
	if err != nil {
		fmt.Fprintln(stderr, cliCopy("workflow", "report.clawdline_report", "clawdline report:"), err)
		return 0
	}
	fmt.Fprintf(stdout, "http://127.0.0.1:%d/reports/%s\n", port, id)
	return 0
}

// localeLanguage is zh-TW for a Chinese locale and English otherwise.
func localeLanguage(getenv func(string) string) string {
	for _, k := range []string{"LC_ALL", "LC_MESSAGES", "LANG"} {
		if v := getenv(k); v != "" {
			if strings.HasPrefix(strings.ToLower(v), "zh") {
				return "zh-TW"
			}
			return "en"
		}
	}
	return "en"
}

// reportPath is where the report goes, and the id the daemon answers it by.
//
// With no --out it is a directory of its own under the state directory's
// reports/, which no repository tracks and the daemon reads
// (turnreport.ReadStored): <date>-<random>/report.html. --out naming an .html
// file is that file; naming anything else is a directory to put
// <date>-<project>.html in, with a number when that name is taken. Neither
// has an id: the daemon answers only its own directory.
func reportPath(out, stateDir, date, project string) (string, string, error) {
	if out == "" {
		id, err := turnreport.NewID(date)
		if err != nil {
			return "", "", err
		}
		dir := filepath.Join(turnreport.ReportsDir(stateDir), id)
		if err := os.MkdirAll(dir, 0o700); err != nil {
			return "", "", err
		}
		return filepath.Join(dir, turnreport.PageName), id, nil
	}
	abs, err := filepath.Abs(out)
	if err != nil {
		return "", "", err
	}
	if strings.EqualFold(filepath.Ext(abs), ".html") {
		return abs, "", nil
	}
	dir := abs
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", "", err
	}
	base := date + "-" + safeName(project)
	for n := 1; ; n++ {
		name := base + ".html"
		if n > 1 {
			name = fmt.Sprintf("%s-%d.html", base, n)
		}
		p := filepath.Join(dir, name)
		if _, err := os.Lstat(p); os.IsNotExist(err) {
			return p, "", nil
		}
	}
}

// safeName keeps a project's name usable as part of a file name.
func safeName(s string) string {
	s = strings.Map(func(r rune) rune {
		if r == '/' || r == '\\' || r == ':' || r < ' ' {
			return '-'
		}
		return r
	}, s)
	if s == "" || s == "." || s == ".." {
		return "report"
	}
	return s
}

// writeReport puts the page in place whole: a reader never opens half of it.
func writeReport(dest string, page []byte) error {
	if err := os.MkdirAll(filepath.Dir(dest), 0o700); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(dest), ".report-*.html")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	if _, err := tmp.Write(page); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), dest)
}

// fileURL is the address a browser opens the file at.
func fileURL(p string) string {
	slash := filepath.ToSlash(p)
	if runtime.GOOS == "windows" && !strings.HasPrefix(slash, "/") {
		slash = "/" + slash
	}
	return (&url.URL{Scheme: "file", Path: slash}).String()
}

func inside(root, p string) bool {
	rel, err := filepath.Rel(root, p)
	return err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) && !filepath.IsAbs(rel)
}
