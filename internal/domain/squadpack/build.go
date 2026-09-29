package squadpack

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"sort"
)

// Build writes a reproducible ZIP and validates it with the same parser used
// for imported archives. Callers choose the files explicitly; no settings or
// session data is gathered implicitly by this package.
func Build(m Manifest, files map[string][]byte) ([]byte, error) {
	m.Format, m.Version = Format, Version
	m.Teams = append([]Definition{}, m.Teams...)
	m.Personas = append([]Definition{}, m.Personas...)
	m.Skills = append([]Definition{}, m.Skills...)
	m.Private = append([]Private(nil), m.Private...)
	normalizeManifest(&m)
	for _, defs := range [][]Definition{m.Teams, m.Personas, m.Skills} {
		for i := range defs {
			body, ok := files[defs[i].Body]
			if !ok {
				return nil, refuse(ErrArchiveMissingFile)
			}
			defs[i].SHA256 = digestOf(body)
		}
	}
	for i := range m.Private {
		body, ok := files[m.Private[i].Path]
		if !ok {
			return nil, refuse(ErrArchiveMissingFile)
		}
		m.Private[i].SHA256 = digestOf(body)
	}
	manifest, err := json.Marshal(m)
	if err != nil {
		return nil, refuse(ErrManifestInvalid)
	}
	if len(manifest) > MaxManifestBytes {
		return nil, refuse(ErrManifestTooLarge)
	}
	var buf bytes.Buffer
	w := zip.NewWriter(&buf)
	write := func(name string, body []byte) error {
		// Store avoids creating a valid-looking archive that the import ratio
		// guard would reject for very repetitive handbooks or skill text.
		h := &zip.FileHeader{Name: name, Method: zip.Store}
		h.SetMode(0644)
		entry, err := w.CreateHeader(h)
		if err != nil {
			return err
		}
		_, err = entry.Write(body)
		return err
	}
	if err := write("manifest.json", manifest); err != nil {
		_ = w.Close()
		return nil, refuse(ErrArchiveInvalid)
	}
	paths := make([]string, 0, len(files))
	for path := range files {
		paths = append(paths, path)
	}
	sort.Strings(paths)
	for _, path := range paths {
		if err := write(path, files[path]); err != nil {
			_ = w.Close()
			return nil, refuse(ErrArchiveInvalid)
		}
	}
	if err := w.Close(); err != nil {
		return nil, refuse(ErrArchiveInvalid)
	}
	if _, err := Parse(buf.Bytes()); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}
