package projectfiles

import (
	"errors"
	"path/filepath"
	"strings"
)

// ErrSensitive refuses the content of a file whose name conventionally holds
// a credential. The browser still lists the name: hiding it would make the
// tree lie about the folder, while its content would reach every authorized
// viewer, including a read-only device that cannot open a terminal.
var ErrSensitive = errors.New("file is named like a credential and is not previewed")

// Names compared case-insensitively against the file's base name.
var sensitiveNames = map[string]bool{
	".envrc": true, ".netrc": true, "_netrc": true, ".npmrc": true, ".pypirc": true,
	".git-credentials": true, ".htpasswd": true, ".pgpass": true, ".dockercfg": true,
	"credentials": true, "credentials.json": true, "service-account.json": true,
	"secrets.json": true, "secrets.yml": true, "secrets.yaml": true, "secrets.toml": true,
	"id_rsa": true, "id_dsa": true, "id_ecdsa": true, "id_ed25519": true,
	"terraform.tfstate": true, "terraform.tfstate.backup": true,
}

// Extensions compared case-insensitively.
var sensitiveExtensions = map[string]bool{
	".pem": true, ".key": true, ".p8": true, ".p12": true, ".pfx": true,
	".jks": true, ".keystore": true, ".ppk": true, ".kdbx": true, ".tfvars": true,
}

// Directories whose every file is a credential store.
var sensitiveDirectories = map[string]bool{".ssh": true, ".gnupg": true, ".aws": true}

// A committed template documents the variables without their values.
var envTemplates = map[string]bool{"example": true, "sample": true, "template": true, "dist": true}

// Sensitive reports whether a Project-relative slash path names a file whose
// content is refused by the tree reader.
func Sensitive(path string) bool {
	parts := strings.Split(strings.ToLower(filepath.ToSlash(path)), "/")
	for _, dir := range parts[:len(parts)-1] {
		if sensitiveDirectories[dir] {
			return true
		}
	}
	name := parts[len(parts)-1]
	if name == ".env" {
		return true
	}
	if rest, ok := strings.CutPrefix(name, ".env."); ok {
		return !envTemplates[rest]
	}
	return sensitiveNames[name] || sensitiveExtensions[filepath.Ext(name)]
}
