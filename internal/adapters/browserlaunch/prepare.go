package browserlaunch

import (
	"bytes"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/sainteye/clawdline/extension"
)

// PreparedExtension describes files only. It is not a Chrome installation or
// evidence that Chrome has loaded this revision.
type PreparedExtension struct {
	OK             bool   `json:"ok"`
	Directory      string `json:"directory"`
	ExtensionID    string `json:"extension_id"`
	Revision       string `json:"revision"`
	Platform       string `json:"platform"`
	OpenCommand    string `json:"open_command,omitempty"`
	ConnectCommand string `json:"connect_command"`
}

// PrepareUserChrome publishes a complete, content-addressed payload. Repeated
// requests verify and reuse it without replacing files Chrome may be reading.
// Native host registration remains a separate person-authorized operation.
func PrepareUserChrome(dir string) (PreparedExtension, error) {
	var receipt PreparedExtension
	if dir == "" {
		return receipt, fmt.Errorf("the Clawdline directory is unavailable")
	}
	target := ExtensionDir(dir)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return receipt, err
	}
	if _, err := os.Lstat(target); os.IsNotExist(err) {
		staging, err := os.MkdirTemp(dir, ".chrome-prepare-")
		if err != nil {
			return receipt, err
		}
		defer os.RemoveAll(staging)
		if err := WriteExtension(filepath.Join(staging, "payload")); err != nil {
			return receipt, err
		}
		if err := os.Rename(filepath.Join(staging, "payload"), target); err != nil {
			// Another request may have published this exact revision first. Verify
			// that winner below; never remove or overwrite an existing directory.
			if _, statErr := os.Lstat(target); statErr != nil {
				return receipt, err
			}
		}
	} else if err != nil {
		return receipt, err
	}
	if err := verifyPreparedExtension(target); err != nil {
		return receipt, err
	}
	directory, err := filepath.EvalSymlinks(target)
	if err != nil {
		return receipt, err
	}
	directory, err = filepath.Abs(directory)
	if err != nil {
		return receipt, err
	}
	id := UnpackedExtensionID(directory)
	receipt = PreparedExtension{OK: true, Directory: directory, ExtensionID: id, Revision: extensionRevision(), Platform: runtime.GOOS,
		ConnectCommand: "clawdline browser install-host --mode user_chrome --extension-id " + id}
	if runtime.GOOS == "darwin" {
		receipt.OpenCommand = "open " + shellPath(directory)
	}
	return receipt, nil
}

func shellPath(path string) string { return "'" + strings.ReplaceAll(path, "'", "'\\''") + "'" }

func verifyPreparedExtension(dir string) error {
	return filepath.WalkDir(dir, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.Type()&os.ModeSymlink != 0 {
			return fmt.Errorf("the prepared extension contains a symbolic link")
		}
		rel, err := filepath.Rel(dir, path)
		if err != nil {
			return err
		}
		name := "chrome"
		if rel != "." {
			name += "/" + filepath.ToSlash(rel)
		}
		expected, err := fs.Stat(extension.Chrome, name)
		if err != nil || entry.IsDir() != expected.IsDir() {
			return fmt.Errorf("the prepared extension differs from this build")
		}
		if !entry.IsDir() && !entry.Type().IsRegular() {
			return fmt.Errorf("the prepared extension contains a non-regular file")
		}
		if entry.IsDir() {
			actual, err := os.ReadDir(path)
			if err != nil {
				return err
			}
			wanted, err := fs.ReadDir(extension.Chrome, name)
			if err != nil || len(actual) != len(wanted) {
				return fmt.Errorf("the prepared extension is incomplete")
			}
			return nil
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		wanted, err := fs.ReadFile(extension.Chrome, name)
		if err != nil || !bytes.Equal(data, wanted) {
			return fmt.Errorf("the prepared extension differs from this build")
		}
		return nil
	})
}
