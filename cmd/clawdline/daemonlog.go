package main

import (
	"fmt"
	"log"
	"os"

	"github.com/sainteye/clawdline/internal/adapters/logs"
	"github.com/sainteye/clawdline/internal/adapters/swiftstore"
	"github.com/sainteye/clawdline/internal/config"
	"github.com/sainteye/clawdline/internal/domain/capacity"
	httptransport "github.com/sainteye/clawdline/internal/transport/http"
)

// daemonLog moves this process's log from stderr into the state directory,
// CLAWDLINE_NEXT_DIR/logs/daemon.log, where it rotates at the register's
// `log.daemon` size and keeps a bounded tail (docs/limits.md N29).
//
// Stderr was wherever whoever started the daemon pointed it, and nothing ever
// shortened that file. It now gets one line saying where the log went, and
// every line after it only when stderr is a terminal, because then somebody is
// watching it and it is not a file. A daemon whose log directory cannot be
// opened keeps logging to stderr, and the register's row says the log is not
// being measured.
func daemonLog(cfg config.Config) {
	w, err := logs.Open(cfg.Dir, append(foreignDirs(), swiftstore.Dir())...)
	if err != nil {
		log.Printf("log: staying on stderr, because the log directory could not be opened: %v", err)
		return
	}
	if stderrIsTerminal() {
		w.SetMirror(os.Stderr)
	}
	fmt.Fprintf(os.Stderr, "clawdline: logging to %s\n", w.Path())
	log.SetOutput(w)
	// After the move, so that anything resolving the overrides says so in
	// the file.
	w.SetLimit(httptransport.CapacityLimit(capacity.LogDaemon))
	httptransport.SetDaemonLog(cfg.Dir, w)
}

func stderrIsTerminal() bool {
	info, err := os.Stderr.Stat()
	return err == nil && info.Mode()&os.ModeCharDevice != 0
}
