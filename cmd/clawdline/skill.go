package main

import (
	"fmt"
	"io"
	"log"
	"os"
	"strings"
	"time"

	"github.com/sainteye/clawdline-go/internal/adapters/skillfile"
	"github.com/sainteye/clawdline-go/internal/config"
	"github.com/sainteye/clawdline-go/skills"
)

// `clawdline guide` and `clawdline skill`: the guide a session reads, and the
// stub that tells a session how to read it.
//
// The guide is compiled into this binary (package skills), so reading it
// needs no daemon, no network and no file on disk, and answers the same over
// SSH and on Linux or Windows. The stub is installed only when a person runs
// `clawdline skill install`: it replaces a file in their own assistant
// configuration, which is theirs to decide (design-guidelines DG-10).

func guideCommand(args []string) {
	os.Exit(printGuide(os.Stdout, os.Stderr, args))
}

// printGuide is the command, answering its exit status.
func printGuide(stdout, stderr io.Writer, args []string) int {
	switch {
	case len(args) > 1:
		fmt.Fprintln(stderr, "usage: clawdline guide [topic] | clawdline guide -list")
		return 2
	case len(args) == 1 && (args[0] == "-list" || args[0] == "--list"):
		for _, t := range skills.Topics() {
			fmt.Fprintln(stdout, t)
		}
		return 0
	case len(args) == 1 && strings.HasPrefix(args[0], "-"):
		fmt.Fprintln(stderr, "usage: clawdline guide [topic] | clawdline guide -list")
		return 2
	}
	topic := ""
	if len(args) == 1 {
		topic = args[0]
	}
	text, err := skills.Guide(topic)
	if err != nil {
		fmt.Fprintln(stderr, "clawdline guide:", err)
		return 1
	}
	_, _ = stdout.Write(text)
	return 0
}

func skillCommand(args []string) {
	if len(args) != 1 {
		skillUsage()
	}
	home, err := os.UserHomeDir()
	if err != nil {
		fail(fmt.Errorf("no home directory: %w", err))
	}
	paths := skillfile.Paths{Home: home, StateDir: config.Dir(), Foreign: foreignDirs()}
	switch args[0] {
	case "install":
		os.Exit(installSkill(os.Stdout, os.Stderr, paths, time.Now()))
	case "uninstall":
		os.Exit(uninstallSkill(os.Stdout, os.Stderr, paths))
	default:
		skillUsage()
	}
}

func skillUsage() {
	fmt.Fprintln(os.Stderr, "usage: clawdline skill <install|uninstall>")
	fmt.Fprintln(os.Stderr, "  install     write this build's stub to ~/.claude/skills/clawdline/SKILL.md, recording what was there")
	fmt.Fprintln(os.Stderr, "  uninstall   put back what install recorded")
	os.Exit(2)
}

func installSkill(stdout, stderr io.Writer, paths skillfile.Paths, now time.Time) int {
	done, err := skillfile.Install(paths, skills.Stub(), now)
	if err != nil {
		fmt.Fprintln(stderr, "clawdline skill install:", err)
		return 1
	}
	switch done.Outcome {
	case skillfile.AlreadyCurrent:
		fmt.Fprintf(stdout, "Already installed: %s is this build's stub. Nothing was changed.\n", done.Path)
		return 0
	case skillfile.Updated:
		fmt.Fprintf(stdout, "Updated %s to this build's stub.\n", done.Path)
	default:
		fmt.Fprintf(stdout, "Installed this build's stub at %s.\n", done.Path)
	}
	fmt.Fprintf(stdout, "What was there before: %s. Recorded in %s.\n", describePrevious(done.Previous), done.RecordPath)
	fmt.Fprintln(stdout, "`clawdline skill uninstall` puts it back. Sessions started from now on load the new stub.")
	return 0
}

func uninstallSkill(stdout, stderr io.Writer, paths skillfile.Paths) int {
	done, err := skillfile.Uninstall(paths)
	if err != nil {
		fmt.Fprintln(stderr, "clawdline skill uninstall:", err)
		return 1
	}
	if !done.Recorded {
		fmt.Fprintf(stdout, "Nothing to undo: there is no install record at %s. %s was left as it is.\n",
			done.RecordPath, done.Path)
		return 0
	}
	fmt.Fprintf(stdout, "Put back what was at %s before the install: %s. The record is removed.\n",
		done.Path, describePrevious(done.Restored))
	return 0
}

// projectBinary keeps <state dir>/bin/clawdline the same bytes as this
// binary, which is the path the skill stub names.
func projectBinary(dir string) {
	self, err := os.Executable()
	if err != nil {
		log.Printf("skill: the stable binary was not updated: this binary cannot find itself: %v", err)
		return
	}
	done, err := skillfile.ProjectBinary(dir, self, foreignDirs()...)
	switch {
	case err != nil:
		log.Printf("skill: the stable binary at %s was not updated: %v", done.Path, err)
	case done.Changed:
		log.Printf("skill: the stable binary at %s is now this build (sha256 %s)", done.Path, done.SHA256)
	}
}

func describePrevious(p skillfile.Previous) string {
	switch p.Kind {
	case skillfile.KindSymlink:
		return "a symbolic link to " + p.LinkTarget
	case skillfile.KindFile:
		return "a file (sha256 " + p.SHA256 + "), kept as a backup"
	case skillfile.KindAbsent:
		return "nothing"
	}
	return "unknown"
}
