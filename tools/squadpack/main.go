// Command squadpack builds a shareable offline ZIP from a local directory.
// Private settings are deliberately excluded; the authenticated Console flow
// handles explicit private scope selection and confirmation.
package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/sainteye/clawdline/internal/domain/cloud"
	"github.com/sainteye/clawdline/internal/domain/squadpack"
)

func main() {
	if len(os.Args) != 3 {
		fmt.Fprintln(os.Stderr, "usage: go run ./tools/squadpack <source-directory> <output.zip>")
		os.Exit(2)
	}
	if err := pack(os.Args[1], os.Args[2]); err != nil {
		fmt.Fprintln(os.Stderr, "squadpack:", err)
		os.Exit(1)
	}
}

func pack(root, output string) error {
	info, err := os.Lstat(root)
	if err != nil {
		return err
	}
	if !info.IsDir() {
		return fmt.Errorf("source is not a regular directory")
	}
	manifestPath := filepath.Join(root, "manifest.json")
	manifestInfo, err := os.Lstat(manifestPath)
	if err != nil {
		return err
	}
	if !manifestInfo.Mode().IsRegular() || manifestInfo.Size() > squadpack.MaxManifestBytes {
		return fmt.Errorf("manifest must be a regular file within the package bound")
	}
	f, err := os.Open(manifestPath)
	if err != nil {
		return err
	}
	manifestBytes, err := io.ReadAll(io.LimitReader(f, squadpack.MaxManifestBytes+1))
	_ = f.Close()
	if err != nil {
		return err
	}
	if len(manifestBytes) > squadpack.MaxManifestBytes {
		return fmt.Errorf("manifest exceeds package bound")
	}
	if _, err := cloud.Parse(manifestBytes); err != nil {
		return fmt.Errorf("manifest is not strict JSON")
	}
	dec := json.NewDecoder(bytes.NewReader(manifestBytes))
	dec.DisallowUnknownFields()
	var manifest squadpack.Manifest
	if err := dec.Decode(&manifest); err != nil {
		return fmt.Errorf("manifest is invalid")
	}
	files := map[string][]byte{}
	var total int64
	err = filepath.WalkDir(root, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if path == root {
			return nil
		}
		if entry.Type()&os.ModeSymlink != 0 {
			return fmt.Errorf("symlink in source directory")
		}
		if entry.IsDir() {
			return nil
		}
		if !entry.Type().IsRegular() {
			return fmt.Errorf("non-regular source entry")
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		rel = filepath.ToSlash(rel)
		if rel == "manifest.json" {
			return nil
		}
		if strings.HasPrefix(rel, "../") || filepath.IsAbs(rel) {
			return fmt.Errorf("source path escapes root")
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		if info.Size() > squadpack.MaxFileBytes {
			return fmt.Errorf("source file exceeds package bound")
		}
		total += info.Size()
		if total > squadpack.MaxExpandedBytes || len(files) >= squadpack.MaxEntries-1 {
			return fmt.Errorf("source exceeds package bound")
		}
		body, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		if len(body) > squadpack.MaxFileBytes {
			return fmt.Errorf("source file changed past bound")
		}
		files[rel] = body
		return nil
	})
	if err != nil {
		return err
	}
	archive, err := squadpack.Export(manifest, files, squadpack.ExportSelection{})
	if err != nil {
		return err
	}
	out, err := os.OpenFile(output, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		return err
	}
	if _, err = out.Write(archive); err != nil {
		_ = out.Close()
		_ = os.Remove(output)
		return err
	}
	if err = out.Close(); err != nil {
		_ = os.Remove(output)
		return err
	}
	fmt.Printf("wrote %s (%d bytes)\n", output, len(archive))
	return nil
}
