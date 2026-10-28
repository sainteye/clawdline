package process

import (
	"net/url"
	"strings"
)

// claudeRemoteWorker recognises the native bridge transport, not an arbitrary
// print-mode SDK job or a command whose prompt merely mentions Remote Control.
func claudeRemoteWorker(args []string) bool {
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
	return printMode && sessionID != "" && err == nil && u.Scheme == "https" &&
		u.Host == "api.anthropic.com" && u.User == nil &&
		u.Path == "/v1/code/sessions/"+sessionID
}
