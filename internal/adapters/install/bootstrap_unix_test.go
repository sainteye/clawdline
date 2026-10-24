//go:build !windows

package install

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"

	"github.com/sainteye/clawdline/internal/adapters/release"
	"github.com/sainteye/clawdline/internal/domain/capacity"
)

func installerTools(t *testing.T) (string, string) {
	t.Helper()
	python, err := exec.LookPath("python3")
	if err != nil {
		t.Fatal("installer verification requires trusted Python 3:", err)
	}
	choices := []string{os.Getenv("CLAWDLINE_INSTALL_OPENSSL"), "openssl", "/opt/homebrew/opt/openssl@3/bin/openssl", "/usr/local/opt/openssl@3/bin/openssl"}
	for _, path := range choices {
		if path == "" {
			continue
		}
		found, err := exec.LookPath(path)
		if err != nil {
			continue
		}
		out, err := exec.Command(found, "version").Output()
		if err == nil && strings.HasPrefix(string(out), "OpenSSL 3.") {
			return python, found
		}
	}
	t.Fatal("installer security tests require trusted OpenSSL 3")
	return "", ""
}

func installerSource(t *testing.T) []byte {
	t.Helper()
	source, err := os.ReadFile("../../../install.sh")
	if err != nil {
		t.Fatal(err)
	}
	return source
}

// Pinning keys and limits across the shell bootstrap and Go release reader is
// a security contract: changing either trust set or ceiling must not silently
// leave first installation with a different policy from signed updates.
func TestInstallerCopiesTheReleaseTrustAndCapacityPolicy(t *testing.T) {
	python, _ := installerTools(t)
	source := string(installerSource(t))
	_, embedded, ok := strings.Cut(source, "<<'CLAWDLINE_VERIFY'\n")
	if !ok {
		t.Fatal("installer has no independently runnable verifier")
	}
	embedded, _, ok = strings.Cut(embedded, "\nCLAWDLINE_VERIFY\n")
	if !ok {
		t.Fatal("installer verifier is incomplete")
	}
	probe := `import ast,json,sys
module=ast.parse(sys.stdin.read())
def value(node):
    if isinstance(node,ast.Constant): return node.value
    if isinstance(node,(ast.Tuple,ast.List)): return [value(n) for n in node.elts]
    if isinstance(node,ast.BinOp) and isinstance(node.op,ast.LShift): return value(node.left)<<value(node.right)
    raise ValueError("not a policy literal")
names={"TRUSTED_KEYS","MANIFEST_BYTES","SIGNATURE_BYTES","ARTIFACT_BYTES","FETCH_SECONDS","DOWNLOAD_SECONDS"}
print(json.dumps({n.targets[0].id:value(n.value) for n in module.body if isinstance(n,ast.Assign) and isinstance(n.targets[0],ast.Name) and n.targets[0].id in names}))`
	cmd := exec.Command(python, "-I", "-c", probe)
	cmd.Stdin = strings.NewReader(embedded)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("read installer policy: %v\n%s", err, out)
	}
	var policy struct {
		Keys      []string `json:"TRUSTED_KEYS"`
		Manifest  int64    `json:"MANIFEST_BYTES"`
		Signature int64    `json:"SIGNATURE_BYTES"`
		Artifact  int64    `json:"ARTIFACT_BYTES"`
		Fetch     int64    `json:"FETCH_SECONDS"`
		Download  int64    `json:"DOWNLOAD_SECONDS"`
	}
	if err := json.Unmarshal(out, &policy); err != nil {
		t.Fatal(err)
	}
	var keys []string
	for _, key := range release.TrustedKeys() {
		keys = append(keys, base64.StdEncoding.EncodeToString(key))
	}
	if !reflect.DeepEqual(policy.Keys, keys) {
		t.Errorf("installer trust differs from release keys: got %v, want %v", policy.Keys, keys)
	}
	for name, got := range map[string]int64{
		capacity.ReleaseManifestBytes: policy.Manifest, capacity.ReleaseSignatureBytes: policy.Signature,
		capacity.ReleaseArtifactBytes: policy.Artifact, capacity.ReleaseFetchTimeoutSeconds: policy.Fetch,
		capacity.ReleaseDownloadTimeoutSeconds: policy.Download,
	} {
		var want int64
		for _, row := range capacity.Register() {
			if row.Name == name {
				want = row.Limit
			}
		}
		if got != want || want == 0 {
			t.Errorf("installer %s = %d, registered limit = %d", name, got, want)
		}
	}
}

func TestInstallerAuthenticatesBeforeExtractingOrExecuting(t *testing.T) {
	_, openssl := installerTools(t)
	for _, scenario := range []string{
		"valid", "changed-manifest", "unknown-signer", "forged-checksum", "truncated-archive",
		"oversized-manifest", "oversized-signatures", "oversized-artifact", "unsafe-name",
		"wrong-version", "missing-openssl", "missing-python", "duplicate-json-field", "unknown-and-trusted-signatures",
		"untrusted-key-environment", "chinese-refusal",
	} {
		t.Run(scenario, func(t *testing.T) {
			home := t.TempDir()
			marker := filepath.Join(home, "executed")
			archive := installerArchive(t)
			pub, signer, err := ed25519.GenerateKey(rand.Reader)
			if err != nil {
				t.Fatal(err)
			}
			_, stranger, err := ed25519.GenerateKey(rand.Reader)
			if err != nil {
				t.Fatal(err)
			}
			manifest := release.Manifest{Version: "v1.2.3", Commit: strings.Repeat("a", 40), Channel: "stable"}
			for _, system := range []string{"darwin", "linux"} {
				for _, arch := range []string{"arm64", "amd64"} {
					sum := sha256.Sum256(archive)
					manifest.Artifacts = append(manifest.Artifacts, release.Artifact{
						OS: system, Arch: arch, Kind: release.KindDaemon,
						Name: fmt.Sprintf("clawdline_v1.2.3_%s_%s.tar.gz", system, arch),
						URL:  "https://example.invalid/release", Size: int64(len(archive)), SHA256: hex.EncodeToString(sum[:]),
					})
				}
			}
			for i := range manifest.Artifacts {
				if scenario == "unsafe-name" {
					manifest.Artifacts[i].Name = "../../outside.tar.gz"
				}
				if scenario == "oversized-artifact" {
					manifest.Artifacts[i].Size = (512 << 20) + 1
				}
			}
			mb, err := json.MarshalIndent(manifest, "", "  ")
			if err != nil {
				t.Fatal(err)
			}
			if scenario == "duplicate-json-field" {
				mb = bytes.Replace(mb, []byte(`"version": "v1.2.3",`), []byte(`"version": "v9.9.9", "version": "v1.2.3",`), 1)
			}
			if scenario == "oversized-manifest" {
				mb = append(mb, bytes.Repeat([]byte(" "), int(release.MaxManifestBytes()))...)
			}
			sigs := []release.Signature{release.Sign(mb, signer)}
			if scenario == "unknown-signer" || scenario == "untrusted-key-environment" {
				sigs = []release.Signature{release.Sign(mb, stranger)}
			}
			if scenario == "unknown-and-trusted-signatures" {
				sigs = append([]release.Signature{release.Sign(mb, stranger)}, sigs...)
			}
			sb, _ := json.Marshal(sigs)
			if scenario == "changed-manifest" || scenario == "chinese-refusal" {
				mb = append(mb, '\n')
			}
			if scenario == "oversized-signatures" {
				sb = append(sb, bytes.Repeat([]byte(" "), int(release.MaxSignatureBytes()))...)
			}
			served := archive
			if scenario == "forged-checksum" {
				served = append(append([]byte{}, archive...), []byte("attacker changed the archive")...)
			}
			if scenario == "truncated-archive" {
				served = archive[:len(archive)-1]
			}
			var mu sync.Mutex
			var requests []string
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				mu.Lock()
				requests = append(requests, r.URL.Path)
				mu.Unlock()
				switch r.URL.Path {
				case "/manifest.json":
					_, _ = w.Write(mb)
				case "/manifest.sig.json":
					_, _ = w.Write(sb)
				case "/SHA256SUMS":
					// A compromised host can replace both the archive and its
					// unsigned checksums. These must never authorize execution.
					sum := sha256.Sum256(served)
					for _, a := range manifest.Artifacts {
						_, _ = fmt.Fprintf(w, "%x  %s\n", sum, a.Name)
					}
				default:
					_, _ = w.Write(served)
				}
			}))
			defer server.Close()
			source := string(installerSource(t))
			for _, key := range release.TrustedKeys() {
				source = strings.ReplaceAll(source, base64.StdEncoding.EncodeToString(key), base64.StdEncoding.EncodeToString(pub))
			}
			script := filepath.Join(home, "install.sh")
			if err := os.WriteFile(script, []byte(source), 0o600); err != nil {
				t.Fatal(err)
			}
			args := []string{script}
			if scenario == "wrong-version" {
				args = append(args, "--version", "v9.9.9")
			}
			cmd := exec.Command("sh", args...)
			cmd.Env = append(os.Environ(), "HOME="+home, "XDG_DATA_HOME="+filepath.Join(home, "data"), "CLAWDLINE_NEXT_DIR="+filepath.Join(home, "state"),
				"CLAWDLINE_NEXT_INSTALL_ROOT="+filepath.Join(home, "install"), "CLAWDLINE_INSTALL_BASE_URL="+server.URL,
				"CLAWDLINE_INSTALL_OPENSSL="+openssl, "CLAWDLINE_INSTALL_TEST_MARKER="+marker, "CLAWDLINE_LANG=en")
			if scenario == "missing-python" {
				bin := filepath.Join(home, "bin")
				if err := os.Mkdir(bin, 0o700); err != nil {
					t.Fatal(err)
				}
				for _, tool := range []string{"uname", "tar"} {
					path, err := exec.LookPath(tool)
					if err != nil {
						t.Fatal(err)
					}
					if err := os.Symlink(path, filepath.Join(bin, tool)); err != nil {
						t.Fatal(err)
					}
				}
				cmd.Env = append(cmd.Env, "PATH="+bin)
			}
			if scenario == "missing-openssl" {
				cmd.Env = append(cmd.Env, "CLAWDLINE_INSTALL_OPENSSL="+filepath.Join(home, "missing-openssl"))
			}
			if scenario == "untrusted-key-environment" {
				cmd.Env = append(cmd.Env, "CLAWDLINE_INSTALL_PUBLIC_KEY="+base64.StdEncoding.EncodeToString(stranger.Public().(ed25519.PublicKey)))
			}
			if scenario == "chinese-refusal" {
				cmd.Env = append(cmd.Env, "CLAWDLINE_LANG=zh-Hant")
			}
			out, runErr := cmd.CombinedOutput()
			_, markerErr := os.Stat(marker)
			valid := scenario == "valid" || scenario == "unknown-and-trusted-signatures"
			if valid {
				if runErr != nil || markerErr != nil {
					t.Fatalf("authenticated release did not execute: %v, marker %v\n%s", runErr, markerErr, out)
				}
				return
			}
			if runErr == nil || !os.IsNotExist(markerErr) {
				t.Fatalf("untrusted release executed or install succeeded: err=%v marker=%v\n%s", runErr, markerErr, out)
			}
			entries, err := os.ReadDir(filepath.Join(home, "install", "releases"))
			if err == nil && len(entries) != 0 {
				t.Fatalf("unverified archive was extracted into releases: %v\n%s", entries, out)
			}
			if scenario == "chinese-refusal" && !strings.Contains(string(out), "未解壓縮或執行任何下載程式") {
				t.Fatalf("Chinese refusal missing: %s", out)
			}
			if scenario != "forged-checksum" && scenario != "truncated-archive" {
				mu.Lock()
				defer mu.Unlock()
				for _, path := range requests {
					if strings.HasSuffix(path, ".tar.gz") {
						t.Fatalf("archive requested before manifest authorization: %v\n%s", requests, out)
					}
				}
			}
		})
	}
}

func installerArchive(t *testing.T) []byte {
	t.Helper()
	var out bytes.Buffer
	gz := gzip.NewWriter(&out)
	tw := tar.NewWriter(gz)
	program := []byte("#!/bin/sh\nprintf 'executed\\n' > \"$CLAWDLINE_INSTALL_TEST_MARKER\"\n")
	if err := tw.WriteHeader(&tar.Header{Name: "clawdline", Mode: 0o755, Size: int64(len(program)), Typeflag: tar.TypeReg}); err != nil {
		t.Fatal(err)
	}
	if _, err := tw.Write(program); err != nil {
		t.Fatal(err)
	}
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := gz.Close(); err != nil {
		t.Fatal(err)
	}
	return out.Bytes()
}
