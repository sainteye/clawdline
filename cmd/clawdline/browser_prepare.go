package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"path/filepath"

	"github.com/sainteye/clawdline/internal/adapters/browserlaunch"
	"github.com/sainteye/clawdline/internal/config"
)

// browserPrepare writes only Clawdline's own extension payload. Registering a
// host in the person's Chrome is the separate command printed in its receipt.
func browserPrepare(args []string, out, errOut io.Writer) int {
	flags := flag.NewFlagSet("browser prepare", flag.ContinueOnError)
	flags.SetOutput(errOut)
	mode := flags.String("mode", "", "user_chrome")
	if flags.Parse(args) != nil {
		return 2
	}
	if *mode != "user_chrome" || flags.NArg() != 0 {
		fmt.Fprintln(errOut, "usage: clawdline browser prepare --mode user_chrome")
		return 2
	}
	directory := browserlaunch.ExtensionDir(config.Dir())
	if err := browserlaunch.WriteExtension(directory); err != nil {
		fmt.Fprintln(errOut, err)
		return 1
	}
	directory, err := filepath.EvalSymlinks(directory)
	if err != nil {
		fmt.Fprintln(errOut, err)
		return 1
	}
	id := browserlaunch.UnpackedExtensionID(directory)
	command := "clawdline browser install-host --mode user_chrome --extension-id " + id
	if err := json.NewEncoder(out).Encode(map[string]any{"ok": true, "directory": directory, "extension_id": id, "connect_command": command}); err != nil {
		fmt.Fprintln(errOut, err)
		return 1
	}
	return 0
}
