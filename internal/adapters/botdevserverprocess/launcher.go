// Package botdevserverprocess launches approved foreground commands through
// the existing identity-checked process owner. It supplies no approval itself.
package botdevserverprocess

import (
	"context"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/sainteye/clawdline/internal/adapters/ownrecords"
	acp "github.com/sainteye/clawdline/internal/adapters/projectassistantacp"
	servers "github.com/sainteye/clawdline/internal/app/botdevserver"
	pa "github.com/sainteye/clawdline/internal/domain/projectassistant"
)

type Launcher struct {
	limits      acp.Limits
	environment []string
}

func failure(code pa.ErrorCode, field string) error { return &pa.Failure{Code: code, Field: field} }

// New snapshots only the source's basic command environment. Callers must
// supply the daemon's own environment, never a provider/profile configuration.
// Credential, shell-hook and runtime-injection variables are not inherited.
func New(limits acp.Limits, sourceEnvironment []string) (*Launcher, error) {
	if err := limits.Validate(); err != nil {
		return nil, err
	}
	allowed := map[string]bool{"PATH": true, "HOME": true, "LANG": true, "LC_ALL": true, "LC_CTYPE": true,
		"USER": true, "LOGNAME": true, "TMPDIR": true, "TMP": true, "TEMP": true, "TERM": true}
	values := map[string]string{}
	for _, raw := range sourceEnvironment {
		name, value, ok := strings.Cut(raw, "=")
		if !ok || !allowed[name] {
			continue
		}
		if strings.ContainsRune(value, 0) {
			return nil, failure(pa.Invalid, "server_environment")
		}
		values[name] = value
	}
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	environment := make([]string, 0, len(keys))
	for _, key := range keys {
		environment = append(environment, key+"="+values[key])
	}
	return &Launcher{limits: limits, environment: environment}, nil
}

func within(root, directory string) bool {
	relative, err := filepath.Rel(root, directory)
	return err == nil && relative != ".." && !strings.HasPrefix(relative, ".."+string(filepath.Separator))
}

// ResolveDirectory normalizes a tool's optional folder against a freshly
// source-resolved Project. The returned canonical path is part of the command
// review/fingerprint. It checks the starting directory, not a shell sandbox.
func ResolveDirectory(project pa.ProjectResolution, folder string) (string, error) {
	if err := pa.ValidateTarget(pa.ProjectSelection{MachineID: project.Scope.MachineID, ProjectID: project.Scope.ProjectID}, project); err != nil {
		return "", err
	}
	if !filepath.IsAbs(project.Directory) {
		return "", failure(pa.Invalid, "server_project_directory")
	}
	root, err := filepath.EvalSymlinks(project.Directory)
	if err != nil {
		return "", failure(pa.Invalid, "server_project_directory")
	}
	directory := root
	if folder != "" {
		if filepath.IsAbs(folder) {
			directory = filepath.Clean(folder)
			// The source's registered path may itself be a symlink (for
			// example a platform's temporary-directory alias). Translate an
			// absolute child of that trusted spelling into its canonical root.
			original := filepath.Clean(project.Directory)
			if within(original, directory) {
				relative, _ := filepath.Rel(original, directory)
				directory = filepath.Join(root, relative)
			}
		} else {
			directory = filepath.Join(root, folder)
		}
	}
	if !within(root, directory) {
		return "", failure(pa.ScopeDenied, "server_directory")
	}
	directory, err = filepath.EvalSymlinks(directory)
	if err != nil {
		return "", failure(pa.Invalid, "server_directory")
	}
	if !within(root, directory) {
		return "", failure(pa.ScopeDenied, "server_directory")
	}
	info, err := os.Stat(directory)
	if err != nil || !info.IsDir() {
		return "", failure(pa.Invalid, "server_directory")
	}
	return directory, nil
}

// Start receives a trusted, current Project resolution, not tool-supplied
// authority. Registry owns durable acceptance and exact-command approval.
// Like the upstream host command, the process runs with source-user authority;
// the directory check does not restrict the command's files or network.
func (l *Launcher) Start(ctx context.Context, spec servers.Spec, project pa.ProjectResolution) (servers.Process, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if err := spec.Validate(); err != nil {
		return nil, err
	}
	if spec.Project != project.Scope {
		return nil, failure(pa.ScopeDenied, "server_project")
	}
	directory, err := ResolveDirectory(project, spec.Directory)
	if err != nil {
		return nil, err
	}
	if directory != spec.Directory {
		return nil, failure(pa.StaleVersion, "server_directory")
	}
	configuration, err := command(spec.Command, append(append([]string{}, l.environment...), "PWD="+directory))
	if err != nil {
		return nil, err
	}
	p, err := acp.StartProcess(ctx, acp.ProcessOptions{Configuration: configuration, Directory: directory, Limits: l.limits, CombinedOutput: true})
	if p == nil {
		return nil, err // Do not return a non-nil interface holding a nil handle.
	}
	return &launched{Process: p, mark: markOf(p.PID())}, err // Even a failed startup retains every acquired handle.
}

// launched keeps every acp.Process method and adds the identity a later Host
// needs to adopt this process after a crash.
type launched struct {
	*acp.Process
	mark servers.Mark
}

func (l *launched) Mark() (servers.Mark, bool) { return l.mark, l.mark.PID > 0 && l.mark.Started > 0 }

func markOf(pid int) servers.Mark {
	started := ownrecords.ProcessStart(pid)
	if pid <= 0 || started.IsZero() {
		return servers.Mark{}
	}
	return servers.Mark{PID: pid, Started: started.Unix()}
}
