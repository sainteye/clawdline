package codegraph

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"crypto/sha512"
	_ "embed"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"
)

//go:embed runtime-lock.json
var runtimeLock []byte

//go:embed LICENSE.upstream
var runtimeLicense []byte

//go:embed bridge.cjs
var bridgeSource []byte

type runtimePackage struct {
	URL       string `json:"url"`
	Integrity string `json:"integrity"`
}
type runtimeManifest struct {
	Version  string                    `json:"version"`
	Packages map[string]runtimePackage `json:"packages"`
}
type runtimePaths struct{ Bundle, Node, Bridge string }

func parserEnvironment(home string) []string {
	env := []string{"PATH=" + os.Getenv("PATH"), "HOME=" + home, "USERPROFILE=" + home, "TMPDIR=" + home, "TMP=" + home, "TEMP=" + home, "LC_ALL=C", "NO_COLOR=1", "DO_NOT_TRACK=1", "CODEGRAPH_TELEMETRY=0", "CODEGRAPH_NO_UPDATE_CHECK=1", "CODEGRAPH_NO_DOWNLOAD=1", "CODEGRAPH_NO_DAEMON=1", "CODEGRAPH_PARSE_WORKERS=1", "CODEGRAPH_RESOLVE_WORKERS=1"}
	if runtime.GOOS == "windows" {
		env = append(env, "SystemRoot="+os.Getenv("SystemRoot"))
	}
	return env
}

type cappedOutput struct {
	mu   sync.Mutex
	data []byte
}

func (w *cappedOutput) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if len(p) >= BuildLogBytesLimit {
		w.data = append(w.data[:0], p[len(p)-BuildLogBytesLimit:]...)
	} else {
		if excess := len(w.data) + len(p) - BuildLogBytesLimit; excess > 0 {
			w.data = append(w.data[:0], w.data[excess:]...)
		}
		w.data = append(w.data, p...)
	}
	return len(p), nil
}

func (b *Builder) runtime(ctx context.Context, download bool) (runtimePaths, error) {
	b.runtimeMu.Lock()
	defer b.runtimeMu.Unlock()
	var lock runtimeManifest
	if json.Unmarshal(runtimeLock, &lock) != nil || lock.Version != Version {
		return runtimePaths{}, BuildError("runtime_unverified")
	}
	platform, arch := runtime.GOOS, runtime.GOARCH
	if platform == "windows" {
		platform = "win32"
	}
	if arch == "amd64" {
		arch = "x64"
	}
	pkg, ok := lock.Packages[platform+"-"+arch]
	if !ok {
		return runtimePaths{}, BuildError("unsupported_platform")
	}
	digest := sha256.Sum256(runtimeLock)
	bundle := filepath.Join(b.cache, "runtime-"+hex.EncodeToString(digest[:]))
	node := filepath.Join(bundle, "node")
	if runtime.GOOS == "windows" {
		node += ".exe"
	}
	ready := filepath.Join(bundle, "clawdline-runtime.json")
	if bytes, err := os.ReadFile(ready); err != nil || string(bytes) != string(runtimeLock) {
		if _, err := os.Lstat(bundle); !os.IsNotExist(err) {
			return runtimePaths{}, BuildError("runtime_unverified")
		}
		if !download {
			return runtimePaths{}, BuildError("runtime_missing")
		}
		if err := os.MkdirAll(b.cache, 0700); err != nil {
			return runtimePaths{}, err
		}
		stage, err := os.MkdirTemp(b.cache, ".runtime-")
		if err != nil {
			return runtimePaths{}, err
		}
		defer os.RemoveAll(stage)
		if err = downloadRuntime(ctx, pkg, stage); err != nil {
			return runtimePaths{}, err
		}
		if err = os.WriteFile(filepath.Join(stage, "clawdline-runtime.json"), runtimeLock, 0600); err != nil {
			return runtimePaths{}, err
		}
		if err = os.Rename(stage, bundle); err != nil {
			return runtimePaths{}, err
		}
	}
	if st, err := os.Lstat(node); err != nil || !st.Mode().IsRegular() {
		return runtimePaths{}, BuildError("runtime_unverified")
	}
	license := filepath.Join(bundle, "CLAWDLINE-CodeGraph-LICENSE.txt")
	if raw, err := os.ReadFile(license); err != nil {
		if !os.IsNotExist(err) {
			return runtimePaths{}, BuildError("runtime_unverified")
		}
		if err = os.WriteFile(license, runtimeLicense, 0600); err != nil {
			return runtimePaths{}, err
		}
	} else if string(raw) != string(runtimeLicense) {
		return runtimePaths{}, BuildError("runtime_unverified")
	}
	bridgeHash := sha256.Sum256(bridgeSource)
	bridge := filepath.Join(b.cache, "bridge-"+hex.EncodeToString(bridgeHash[:])+".cjs")
	if raw, err := os.ReadFile(bridge); err != nil || string(raw) != string(bridgeSource) {
		if err = os.WriteFile(bridge, bridgeSource, 0600); err != nil {
			return runtimePaths{}, err
		}
	}
	return runtimePaths{Bundle: bundle, Node: node, Bridge: bridge}, nil
}

func downloadRuntime(ctx context.Context, pkg runtimePackage, destination string) error {
	if !strings.HasPrefix(pkg.URL, "https://registry.npmjs.org/@colbymchenry/codegraph-") {
		return BuildError("runtime_unverified")
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, pkg.URL, nil)
	if err != nil {
		return err
	}
	client := &http.Client{Timeout: time.Duration(BuildSecondsLimit) * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return errors.New("redirect refused") }}
	response, err := client.Do(request)
	if err != nil {
		return BuildError("preparation_failed")
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return BuildError("preparation_failed")
	}
	archive, err := os.CreateTemp(destination, ".download-")
	if err != nil {
		return err
	}
	name := archive.Name()
	defer os.Remove(name)
	h := sha512.New()
	count, err := io.Copy(io.MultiWriter(archive, h), io.LimitReader(response.Body, BuildRawBytesLimit+1))
	archive.Close()
	if err != nil || count > BuildRawBytesLimit {
		return BuildError("runtime_limit")
	}
	if pkg.Integrity != "sha512-"+base64.StdEncoding.EncodeToString(h.Sum(nil)) {
		return BuildError("runtime_unverified")
	}
	input, err := os.Open(name)
	if err != nil {
		return err
	}
	defer input.Close()
	compressed, err := gzip.NewReader(input)
	if err != nil {
		return BuildError("runtime_unverified")
	}
	defer compressed.Close()
	reader := tar.NewReader(compressed)
	var bytes int64
	entries := 0
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		header, err := reader.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return BuildError("runtime_unverified")
		}
		entries++
		if entries > MaxScanEntries {
			return BuildError("runtime_limit")
		}
		if (header.Name == "package/" || header.Name == "package") && header.Typeflag == tar.TypeDir {
			continue
		}
		relative := strings.TrimSuffix(strings.TrimPrefix(header.Name, "package/"), "/")
		if relative == header.Name || relative == "" || relative == ".." || !filepath.IsLocal(filepath.FromSlash(relative)) || path.IsAbs(relative) || path.Clean(relative) != relative || strings.Contains(relative, "\\") || strings.HasPrefix(relative, "../") {
			return BuildError("runtime_unverified")
		}
		target := filepath.Join(destination, filepath.FromSlash(relative))
		switch header.Typeflag {
		case tar.TypeDir:
			if err = os.MkdirAll(target, 0700); err != nil {
				return err
			}
		case tar.TypeReg, tar.TypeRegA:
			bytes += header.Size
			if header.Size < 0 || bytes > RuntimeBytesLimit {
				return BuildError("runtime_limit")
			}
			if err = os.MkdirAll(filepath.Dir(target), 0700); err != nil {
				return err
			}
			mode := os.FileMode(0600)
			if header.Mode&0111 != 0 {
				mode = 0700
			}
			output, err := os.OpenFile(target, os.O_WRONLY|os.O_CREATE|os.O_EXCL, mode)
			if err != nil {
				return err
			}
			_, err = io.Copy(output, reader)
			output.Close()
			if err != nil {
				return err
			}
		default:
			return BuildError("runtime_unverified")
		}
	}
	return nil
}

func bridgeLimits() string {
	raw, _ := json.Marshal(map[string]any{"version": Version, "chunkBytes": BuildLogBytesLimit, "entries": MaxScanEntries, "fileBytes": MaxFileBytes, "totalBytes": MaxTotalBytes, "metadataBytes": MaxMetadataBytes, "storeBytes": StoreBytesLimit, "queryBytes": QueryBytesLimit, "replyBytes": ReplyBytesLimit, "reconcileSeconds": ReconcileSecondsLimit, "extensions": sourceExtensions})
	return string(raw)
}
func (b *Builder) extract(ctx context.Context, input, output string, phase func(string)) error {
	phase("preparing")
	rt, err := b.runtime(ctx, true)
	if err != nil {
		return err
	}
	home := filepath.Join(filepath.Dir(input), "home")
	if err = os.MkdirAll(home, 0700); err != nil {
		return err
	}
	phase("parsing")
	command := exec.CommandContext(ctx, rt.Node, "--liftoff-only", rt.Bridge, rt.Bundle, "build", input, bridgeLimits())
	command.Env = parserEnvironment(home)
	command.Dir = home
	var logs cappedOutput
	command.Stdout = io.Discard
	command.Stderr = &logs
	// Closing the private stdin pipe revokes exactly this child on owner exit.
	read, write, err := os.Pipe()
	if err != nil {
		return err
	}
	defer read.Close()
	defer write.Close()
	command.Stdin = read
	if err = command.Run(); err != nil {
		// Keep one bounded private diagnostic, never vendor output in HTTP errors.
		header := []byte(fmt.Sprint(err) + "\n")
		room := max(0, BuildLogBytesLimit-len(header))
		_ = os.WriteFile(filepath.Join(b.cache, "last-build.log"), append(header, logs.data[max(0, len(logs.data)-room):]...), 0600)
		return BuildError("parser_failed")
	}
	return os.Rename(filepath.Join(input, ".codegraph"), filepath.Join(output, "native"))
}
