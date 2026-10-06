// Command release builds, signs and checks a release's manifest
// (docs/releasing.md). tools/release/build.sh drives it.
//
//	go run ./tools/release keygen <private-key-file>
//	go run ./tools/release manifest -version v -commit c -committed-at t -channel ch -base-url u <dir>
//	go run ./tools/release sign -key <private-key-file> <dir>
//	go run ./tools/release verify [-pub base64] <dir>
//
// The private key file holds the base64 Ed25519 seed and nothing else. It is
// never written inside the repository.
package main

import (
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/sainteye/clawdline/internal/adapters/release"
)

func main() {
	if len(os.Args) < 2 {
		usage()
	}
	var err error
	switch os.Args[1] {
	case "keygen":
		err = keygen(os.Args[2:])
	case "manifest":
		err = manifest(os.Args[2:])
	case "sign":
		err = sign(os.Args[2:])
	case "verify":
		err = verify(os.Args[2:])
	default:
		usage()
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "release:", err)
		os.Exit(1)
	}
}

func usage() {
	fmt.Fprintln(os.Stderr, "usage: release keygen <key-file> | manifest … <dir> | sign -key <key-file> <dir> | verify [-pub base64] <dir>")
	os.Exit(2)
}

func keygen(args []string) error {
	if len(args) != 1 {
		usage()
	}
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return err
	}
	f, err := os.OpenFile(args[0], os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return err
	}
	if _, err := fmt.Fprintln(f, base64.StdEncoding.EncodeToString(priv.Seed())); err != nil {
		f.Close()
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	fmt.Printf("public key %s (key id %s)\n", base64.StdEncoding.EncodeToString(pub), release.KeyID(pub))
	return nil
}

func readKey(path string) (ed25519.PrivateKey, error) {
	var raw string
	if path == "-" || path == "" {
		raw = os.Getenv("CLAWDLINE_RELEASE_KEY")
		if raw == "" {
			return nil, fmt.Errorf("no key: give -key <file> or set CLAWDLINE_RELEASE_KEY")
		}
	} else {
		b, err := os.ReadFile(path)
		if err != nil {
			return nil, err
		}
		raw = string(b)
	}
	seed, err := base64.StdEncoding.DecodeString(strings.TrimSpace(raw))
	if err != nil || len(seed) != ed25519.SeedSize {
		return nil, fmt.Errorf("the key is not a base64 Ed25519 seed")
	}
	return ed25519.NewKeyFromSeed(seed), nil
}

// artifactOf names what a file in the release directory is, from its name:
// clawdline_<v>_<os>_<arch>.tar.gz is a daemon, Clawdline-<v>-macos-<arch>.tar.gz
// the app.
func artifactOf(name, version string) (release.Artifact, bool) {
	if rest, ok := strings.CutPrefix(name, "clawdline_"+version+"_"); ok {
		if p, ok := strings.CutSuffix(rest, ".tar.gz"); ok {
			if osName, arch, ok := strings.Cut(p, "_"); ok {
				return release.Artifact{OS: osName, Arch: arch, Kind: release.KindDaemon, Name: name}, true
			}
		}
	}
	if rest, ok := strings.CutPrefix(name, "Clawdline-"+version+"-macos-"); ok {
		if arch, ok := strings.CutSuffix(rest, ".tar.gz"); ok {
			return release.Artifact{OS: "darwin", Arch: arch, Kind: release.KindApp, Name: name}, true
		}
	}
	return release.Artifact{}, false
}

func manifest(args []string) error {
	fs := flag.NewFlagSet("manifest", flag.ExitOnError)
	version := fs.String("version", "", "release version, vX.Y.Z")
	commit := fs.String("commit", "", "full commit")
	committedAt := fs.String("committed-at", "", "commit time, RFC3339 UTC")
	channel := fs.String("channel", "stable", "stable or beta")
	baseURL := fs.String("base-url", "", "where the artifacts will be downloadable, without the file name")
	minVersion := fs.String("min-version", "", "oldest version this release may be upgraded from")
	notes := fs.String("notes-url", "", "release notes")
	nonAdditive := fs.Bool("non-additive-migration", false, "the store migration cannot be read by an older binary")
	_ = fs.Parse(args)
	if fs.NArg() != 1 || *version == "" || *commit == "" || *baseURL == "" {
		usage()
	}
	dir := fs.Arg(0)
	entries, err := os.ReadDir(dir)
	if err != nil {
		return err
	}
	m := release.Manifest{Version: *version, Commit: *commit, CommittedAt: *committedAt, Channel: *channel,
		MinVersion: *minVersion, NonAdditiveMigration: *nonAdditive, NotesURL: *notes}
	for _, e := range entries {
		a, ok := artifactOf(e.Name(), *version)
		if !ok {
			continue
		}
		f, err := os.Open(filepath.Join(dir, e.Name()))
		if err != nil {
			return err
		}
		h := sha256.New()
		n, err := io.Copy(h, f)
		f.Close()
		if err != nil {
			return err
		}
		a.Size, a.SHA256 = n, hex.EncodeToString(h.Sum(nil))
		a.URL = strings.TrimSuffix(*baseURL, "/") + "/" + e.Name()
		m.Artifacts = append(m.Artifacts, a)
	}
	if len(m.Artifacts) == 0 {
		return fmt.Errorf("%s holds no artifact of %s", dir, *version)
	}
	sort.Slice(m.Artifacts, func(i, j int) bool { return m.Artifacts[i].Name < m.Artifacts[j].Name })
	b, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return err
	}
	b = append(b, '\n')
	if len(b) > int(release.MaxManifestBytes()) {
		return fmt.Errorf("manifest is %d bytes, over the %d a daemon reads", len(b), release.MaxManifestBytes())
	}
	return os.WriteFile(filepath.Join(dir, "manifest.json"), b, 0o644)
}

func sign(args []string) error {
	fs := flag.NewFlagSet("sign", flag.ExitOnError)
	keyFile := fs.String("key", "", "private key file; empty reads CLAWDLINE_RELEASE_KEY")
	_ = fs.Parse(args)
	if fs.NArg() != 1 {
		usage()
	}
	priv, err := readKey(*keyFile)
	if err != nil {
		return err
	}
	dir := fs.Arg(0)
	m, err := os.ReadFile(filepath.Join(dir, "manifest.json"))
	if err != nil {
		return err
	}
	// An existing list keeps its other keys' signatures: a rotation signs twice.
	var list []release.Signature
	if b, err := os.ReadFile(filepath.Join(dir, "manifest.sig.json")); err == nil {
		if err := json.Unmarshal(b, &list); err != nil {
			return fmt.Errorf("existing manifest.sig.json: %v", err)
		}
	}
	s := release.Sign(m, priv)
	kept := list[:0]
	for _, o := range list {
		if o.KeyID != s.KeyID {
			kept = append(kept, o)
		}
	}
	list = append(kept, s)
	b, err := json.MarshalIndent(list, "", "  ")
	if err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(dir, "manifest.sig.json"), append(b, '\n'), 0o644); err != nil {
		return err
	}
	fmt.Printf("signed with key %s\n", s.KeyID)
	return nil
}

func verify(args []string) error {
	fs := flag.NewFlagSet("verify", flag.ExitOnError)
	pub := fs.String("pub", "", "a base64 public key to trust in place of the compiled-in ones")
	_ = fs.Parse(args)
	if fs.NArg() != 1 {
		usage()
	}
	dir := fs.Arg(0)
	keys := release.TrustedKeys()
	if *pub != "" {
		raw, err := base64.StdEncoding.DecodeString(*pub)
		if err != nil || len(raw) != ed25519.PublicKeySize {
			return fmt.Errorf("-pub is not a base64 Ed25519 public key")
		}
		keys = []ed25519.PublicKey{raw}
	}
	mb, err := os.ReadFile(filepath.Join(dir, "manifest.json"))
	if err != nil {
		return err
	}
	sb, err := os.ReadFile(filepath.Join(dir, "manifest.sig.json"))
	if err != nil {
		return err
	}
	m, err := release.Open(mb, sb, keys)
	if err != nil {
		return err
	}
	for _, a := range m.Artifacts {
		f, err := os.Open(filepath.Join(dir, a.Name))
		if err != nil {
			return err
		}
		err = a.Check(f, nil)
		f.Close()
		if err != nil {
			return err
		}
		fmt.Printf("ok %s %s/%s %s %d bytes\n", a.Kind, a.OS, a.Arch, a.Name, a.Size)
	}
	fmt.Printf("release %s (%s) verified: %d artifacts\n", m.Version, m.Commit[:12], len(m.Artifacts))
	return nil
}
