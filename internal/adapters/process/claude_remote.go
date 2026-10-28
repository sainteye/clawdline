package process

import (
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// claudeRemoteWorker recognises the native bridge transport, not an arbitrary
// print-mode SDK job or a command whose prompt merely mentions Remote Control.
func claudeRemoteWorker(args []string) bool { return RemoteControlID(args) != "" }

var remoteIDPattern = regexp.MustCompile(`^(cse|session)_[A-Za-z0-9]+$`)

// NormalizeRemoteID accepts only a native session address, never a path or URL.
func NormalizeRemoteID(id string) string {
	if !remoteIDPattern.MatchString(id) {
		return ""
	}
	return "cse_" + strings.TrimPrefix(strings.TrimPrefix(id, "cse_"), "session_")
}

// RemoteControlID reads the native worker transport from process arguments.
func RemoteControlID(args []string) string {
	printMode, sdkURL, sessionID := false, "", ""
	for i := 0; i < len(args); i++ {
		arg := args[i]
		switch arg {
		case "--print", "-p":
			printMode = true
		case "--sdk-url", "--session-id":
			if i+1 < len(args) {
				i++
				if arg == "--sdk-url" {
					sdkURL = args[i]
				} else {
					sessionID = args[i]
				}
			}
		default:
			if strings.HasPrefix(arg, "--sdk-url=") {
				sdkURL = strings.TrimPrefix(arg, "--sdk-url=")
			}
			if strings.HasPrefix(arg, "--session-id=") {
				sessionID = strings.TrimPrefix(arg, "--session-id=")
			}
		}
	}
	u, err := url.Parse(sdkURL)
	if printMode && NormalizeRemoteID(sessionID) != "" && err == nil && u.Scheme == "https" &&
		u.Host == "api.anthropic.com" && u.User == nil && u.RawQuery == "" && u.Fragment == "" &&
		u.Path == "/v1/code/sessions/"+sessionID {
		return NormalizeRemoteID(sessionID)
	}
	return ""
}

var claudeNativeVersion = regexp.MustCompile(`^[0-9]+\.[0-9]+\.[0-9]+$`)

// Native installations execute their versioned binary, not a file named claude.
// Only the current user's standard install root is recognized here.
func nativeClaudeRemoteExecutable(executable string) bool {
	home, err := os.UserHomeDir()
	if err != nil {
		return false
	}
	return filepath.Dir(executable) == filepath.Join(home, ".local", "share", "claude", "versions") && claudeNativeVersion.MatchString(filepath.Base(executable))
}
