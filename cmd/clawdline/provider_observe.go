package main

import (
	"context"
	"fmt"
	"io"

	"github.com/sainteye/clawdline/internal/adapters/antigravity"
)

// runProviderObserve is deliberately dependency-injected. Main owns selection
// of the configured canonical private state directory and any managed launch
// capability loader; neither can be supplied by native stdin or arbitrary argv.
// args begins with "antigravity" after main consumes "provider observe".
func runProviderObserve(ctx context.Context, args []string, stateDir string, launch *antigravity.HookLaunch, stdin io.Reader, stdout, stderr io.Writer) int {
	if len(args) != 3 || args[0] != "antigravity" || args[1] != "--event" {
		fmt.Fprint(stderr, cliCopy("provider", "observe.usage", "Usage: clawdline provider observe antigravity --event EVENT\n"))
		return 2
	}
	if err := antigravity.ObserveHook(ctx, stateDir, args[2], launch, stdin, stdout); err != nil {
		// The adapter returns only stable, non-sensitive codes. No native
		// payload, arguments, capability or private path is printed here.
		fmt.Fprintf(stderr, cliCopy("provider", "observe.failed", "Antigravity hook failed (%s).\n"), err.Error())
		return 1
	}
	return 0
}
