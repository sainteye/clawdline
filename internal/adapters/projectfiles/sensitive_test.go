package projectfiles

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

// A credential-named file is listed by name but its content never leaves the
// machine through the tree reader, which Cloud and read-only devices also use.
func TestProjectTreeRefusesCredentialNamedFiles(t *testing.T) {
	place := t.TempDir()
	refused := []string{".env", ".env.local", ".ENV.production", ".envrc", ".npmrc", ".netrc",
		"credentials.json", "id_ed25519", "deploy/server.pem", "certs/tls.KEY", "AuthKey_ABC.p8",
		"infra/prod.tfvars", "terraform.tfstate", "home/.ssh/config", "home/.aws/config"}
	readable := []string{".env.example", ".env.sample", "id_ed25519.pub", "keys.md", "src/env.go", "README.md"}
	for _, name := range append(append([]string{}, refused...), readable...) {
		full := filepath.Join(place, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(full), 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte("API_KEY=sk-test\n"), 0600); err != nil {
			t.Fatal(err)
		}
	}
	for _, name := range refused {
		content, err := ReadTree(place, name)
		if !errors.Is(err, ErrSensitive) || content.Text != "" {
			t.Errorf("%s: read %q, %v; want sensitive refusal", name, content.Text, err)
		}
	}
	for _, name := range readable {
		if _, err := ReadTree(place, name); err != nil {
			t.Errorf("%s: %v; want readable", name, err)
		}
	}
	status, code, _ := Refusal(ErrSensitive)
	if status != 403 || code != "sensitive_file" {
		t.Fatalf("refusal: %d %s", status, code)
	}
	root, err := ListTree(place, "")
	if err != nil {
		t.Fatal(err)
	}
	marked := map[string]bool{}
	for _, entry := range root.Entries {
		marked[entry.Name] = entry.Sensitive
	}
	if !marked[".env"] || marked[".env.example"] || marked["README.md"] {
		t.Fatalf("listing marks: %+v", root.Entries)
	}
}
