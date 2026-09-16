// Command clawdline is the whole product: one binary that is both the daemon and
// the command line that talks to it.
package main

import (
	"fmt"
	"os"

	"github.com/sainteye/clawdline-go/internal/config"
	httptransport "github.com/sainteye/clawdline-go/internal/transport/http"
)

const version = "0.0.1-p0"

func main() {
	if len(os.Args) < 2 {
		usage()
		os.Exit(2)
	}
	switch os.Args[1] {
	case "serve":
		serve()
	case "doctor":
		doctor()
	case "version", "--version", "-v":
		fmt.Println(version)
	default:
		usage()
		os.Exit(2)
	}
}

func serve() {
	cfg := config.Load()
	srv, err := httptransport.New(cfg)
	if err != nil {
		fmt.Fprintln(os.Stderr, "clawdline:", err)
		os.Exit(1)
	}
	if err := srv.ListenAndServe(); err != nil {
		fmt.Fprintln(os.Stderr, "clawdline:", err)
		os.Exit(1)
	}
}

func doctor() {
	cfg := config.Load()
	fmt.Printf("version   %s\n", version)
	fmt.Printf("port      %d\n", cfg.Port)
	fmt.Printf("upstream  %d\n", cfg.UpstreamPort)
	fmt.Printf("dir       %s\n", cfg.Dir)
}

func usage() {
	fmt.Fprintln(os.Stderr, "usage: clawdline <serve|doctor|version>")
}
