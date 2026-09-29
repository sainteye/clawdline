package squadpack

import (
	"archive/zip"
	"bytes"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"regexp"
	"strings"
	"unicode/utf8"

	"github.com/sainteye/clawdline/internal/domain/cloud"
)

var namespacePattern = regexp.MustCompile(`^[a-z][a-z0-9-]*(?:\.[a-z][a-z0-9-]*)+$`)
var versionPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._:+-]{0,99}$`)
var digestPattern = regexp.MustCompile(`^sha256:[0-9a-f]{64}$`)
var colorPattern = regexp.MustCompile(`^#[0-9A-Fa-f]{6}$`)

// Parse verifies the entire ZIP before returning any definition. Callers may
// use the result for a preview, never as authority to write settings.
func Parse(data []byte) (Package, error) {
	if len(data) > MaxArchiveBytes {
		return Package{}, refuse(ErrArchiveTooLarge)
	}
	if len(data) < 22 {
		return Package{}, refuse(ErrArchiveInvalid)
	}
	r, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil || len(r.File) == 0 || r.Comment != "" {
		return Package{}, refuse(ErrArchiveInvalid)
	}
	if len(r.File) > MaxEntries {
		return Package{}, refuse(ErrArchiveEntryCount)
	}
	if err := verifyContainer(data, r.File); err != nil {
		return Package{}, err
	}
	files := make(map[string][]byte, len(r.File))
	var total uint64
	for _, f := range r.File {
		if f.UncompressedSize64 > MaxFileBytes && f.Name != "manifest.json" {
			return Package{}, refuse(ErrArchiveFileTooLarge)
		}
		if f.Name == "manifest.json" && f.UncompressedSize64 > MaxManifestBytes {
			return Package{}, refuse(ErrManifestTooLarge)
		}
		total += f.UncompressedSize64
		if total > MaxExpandedBytes {
			return Package{}, refuse(ErrArchiveExpansion)
		}
		if f.UncompressedSize64 > 0 && (f.CompressedSize64 == 0 ||
			f.UncompressedSize64 > f.CompressedSize64*MaxExpansionRatio) {
			return Package{}, refuse(ErrArchiveExpansion)
		}
		limit := MaxFileBytes
		if f.Name == "manifest.json" {
			limit = MaxManifestBytes
		}
		stream, err := f.Open()
		if err != nil {
			return Package{}, refuse(ErrArchiveInvalid)
		}
		body, readErr := io.ReadAll(io.LimitReader(stream, int64(limit)+1))
		closeErr := stream.Close()
		if readErr != nil || closeErr != nil || uint64(len(body)) != f.UncompressedSize64 {
			return Package{}, refuse(ErrArchiveInvalid)
		}
		if len(body) > limit {
			return Package{}, refuse(ErrArchiveFileTooLarge)
		}
		files[f.Name] = body
	}
	manifestBytes, ok := files["manifest.json"]
	if !ok {
		return Package{}, refuse(ErrArchiveMissingFile)
	}
	manifest, err := parseManifest(manifestBytes)
	if err != nil {
		return Package{}, err
	}
	if err := validateManifest(manifest, files); err != nil {
		return Package{}, err
	}
	delete(files, "manifest.json")
	sum := sha256.Sum256(data)
	return Package{Manifest: manifest, Files: files, Digest: "sha256:" + hex.EncodeToString(sum[:])}, nil
}

// verifyContainer accepts a deliberately narrow ZIP profile. It walks local
// headers independently of archive/zip's central-directory parser so a
// mismatch cannot make two readers interpret different file bytes.
func verifyContainer(data []byte, files []*zip.File) error {
	eocd := data[len(data)-22:]
	if binary.LittleEndian.Uint32(eocd[:4]) != 0x06054b50 ||
		binary.LittleEndian.Uint16(eocd[4:6]) != 0 ||
		binary.LittleEndian.Uint16(eocd[6:8]) != 0 ||
		binary.LittleEndian.Uint16(eocd[8:10]) != uint16(len(files)) ||
		binary.LittleEndian.Uint16(eocd[10:12]) != uint16(len(files)) ||
		binary.LittleEndian.Uint16(eocd[20:22]) != 0 {
		return refuse(ErrArchiveInvalid)
	}
	centralSize := uint64(binary.LittleEndian.Uint32(eocd[12:16]))
	centralAt := uint64(binary.LittleEndian.Uint32(eocd[16:20]))
	if centralSize == 0xffffffff || centralAt == 0xffffffff ||
		centralAt+centralSize != uint64(len(data)-22) {
		return refuse(ErrArchiveInvalid)
	}
	byName := make(map[string]*zip.File, len(files))
	folded := make(map[string]bool, len(files))
	for _, f := range files {
		fold := strings.ToLower(f.Name)
		if folded[fold] {
			return refuse(ErrArchivePathCollision)
		}
		folded[fold] = true
		if !validPath(f.Name) {
			return refuse(ErrArchivePath)
		}
		if f.Flags&1 != 0 {
			return refuse(ErrArchiveEncryption)
		}
		if f.Flags & ^uint16(0x808) != 0 || len(f.Extra) != 0 || f.Comment != "" {
			return refuse(ErrArchiveHeaderMismatch)
		}
		if f.Method != zip.Store && f.Method != zip.Deflate {
			return refuse(ErrArchiveCompression)
		}
		if !f.Mode().IsRegular() {
			return refuse(ErrArchiveEntryType)
		}
		if f.UncompressedSize64 > 0xffffffff || f.CompressedSize64 > 0xffffffff {
			return refuse(ErrArchiveInvalid)
		}
		byName[f.Name] = f
	}
	seen := make(map[string]bool, len(files))
	at := uint64(0)
	for at < centralAt {
		if at+30 > centralAt || binary.LittleEndian.Uint32(data[at:at+4]) != 0x04034b50 {
			return refuse(ErrArchiveHeaderMismatch)
		}
		h := data[at : at+30]
		flags := binary.LittleEndian.Uint16(h[6:8])
		method := binary.LittleEndian.Uint16(h[8:10])
		nameLen := uint64(binary.LittleEndian.Uint16(h[26:28]))
		extraLen := uint64(binary.LittleEndian.Uint16(h[28:30]))
		dataAt := at + 30 + nameLen + extraLen
		if dataAt > centralAt || extraLen != 0 {
			return refuse(ErrArchiveHeaderMismatch)
		}
		name := string(data[at+30 : at+30+nameLen])
		f := byName[name]
		if f == nil || seen[name] || flags != f.Flags || method != f.Method {
			return refuse(ErrArchiveHeaderMismatch)
		}
		seen[name] = true
		fileAt, err := f.DataOffset()
		if err != nil || uint64(fileAt) != dataAt || dataAt+f.CompressedSize64 > centralAt {
			return refuse(ErrArchiveHeaderMismatch)
		}
		at = dataAt + f.CompressedSize64
		if flags&8 != 0 {
			if at+16 > centralAt || binary.LittleEndian.Uint32(data[at:at+4]) != 0x08074b50 ||
				binary.LittleEndian.Uint32(data[at+4:at+8]) != f.CRC32 ||
				uint64(binary.LittleEndian.Uint32(data[at+8:at+12])) != f.CompressedSize64 ||
				uint64(binary.LittleEndian.Uint32(data[at+12:at+16])) != f.UncompressedSize64 {
				return refuse(ErrArchiveHeaderMismatch)
			}
			at += 16
		} else if binary.LittleEndian.Uint32(h[14:18]) != f.CRC32 ||
			uint64(binary.LittleEndian.Uint32(h[18:22])) != f.CompressedSize64 ||
			uint64(binary.LittleEndian.Uint32(h[22:26])) != f.UncompressedSize64 {
			return refuse(ErrArchiveHeaderMismatch)
		}
	}
	if at != centralAt || len(seen) != len(files) {
		return refuse(ErrArchiveHeaderMismatch)
	}
	return nil
}

func validPath(path string) bool {
	if path == "manifest.json" {
		return true
	}
	if path == "" || len(path) > 240 || strings.HasPrefix(path, "/") || strings.ContainsAny(path, "\\:") || strings.Contains(path, "//") {
		return false
	}
	for _, part := range strings.Split(path, "/") {
		if part == "" || part == "." || part == ".." {
			return false
		}
		for _, b := range []byte(part) {
			if !(b >= 'a' && b <= 'z' || b >= '0' && b <= '9' || b == '-' || b == '_' || b == '.') {
				return false
			}
		}
	}
	return true
}

func parseManifest(data []byte) (Manifest, error) {
	if len(data) > MaxManifestBytes {
		return Manifest{}, refuse(ErrManifestTooLarge)
	}
	if _, err := cloud.Parse(data); err != nil {
		return Manifest{}, refuse(ErrManifestInvalid)
	}
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	var m Manifest
	if err := dec.Decode(&m); err != nil {
		return Manifest{}, refuse(ErrManifestInvalid)
	}
	if err := dec.Decode(new(any)); !errors.Is(err, io.EOF) {
		return Manifest{}, refuse(ErrManifestInvalid)
	}
	if m.Format != Format || m.Version != Version {
		return Manifest{}, refuse(ErrFormatUnsupported)
	}
	if m.Teams == nil || m.Personas == nil || m.Skills == nil {
		return Manifest{}, refuse(ErrManifestInvalid)
	}
	normalizeManifest(&m)
	return m, nil
}

func normalizeManifest(m *Manifest) {
	for _, defs := range [][]Definition{m.Teams, m.Personas, m.Skills} {
		for i := range defs {
			if defs[i].Source == "" {
				defs[i].Source = m.Source
			}
			if defs[i].License == "" {
				defs[i].License = m.License
			}
		}
	}
}

func validateManifest(m Manifest, files map[string][]byte) error {
	if !namespacePattern.MatchString(m.Namespace) || strings.HasPrefix(m.Namespace, "clawdline.") ||
		m.Namespace == "clawdline" || m.Source == "" || m.License == "" {
		return refuse(ErrManifestInvalid)
	}
	ids := map[string]string{}
	names := map[string]bool{}
	wanted := map[string]bool{"manifest.json": true}
	check := func(kind string, defs []Definition) error {
		for _, d := range defs {
			if _, exists := ids[d.ID]; exists {
				return refuse(ErrDuplicateID)
			}
			if strings.HasPrefix(d.ID, "clawdline.") {
				return refuse(ErrReservedID)
			}
			if !namespacePattern.MatchString(d.ID) ||
				!versionPattern.MatchString(d.Version) || strings.TrimSpace(d.Name) == "" ||
				len(d.Name) > 200 || len(d.NameZhHant) > 200 ||
				len(d.Summary) > 2000 || len(d.SummaryZhHant) > 2000 ||
				len(d.Purpose) > 2000 || len(d.PurposeZhHant) > 2000 ||
				(kind != "persona" && (d.Summary != "" || d.SummaryZhHant != "")) ||
				(kind != "skill" && (d.Purpose != "" || d.PurposeZhHant != "")) ||
				!validIcon(d.Icon) || !validPath(d.Body) ||
				!strings.HasPrefix(d.Body, "definitions/") || !digestPattern.MatchString(d.SHA256) ||
				(kind != "team" && len(d.Personas) != 0) ||
				(kind != "persona" && (len(d.Skills) != 0 || len(d.DisabledSkills) != 0)) {
				return refuse(ErrInvalidDefinition)
			}
			nameKey := kind + "\x00" + strings.ToLower(d.Name)
			if names[nameKey] {
				return refuse(ErrNameConflict)
			}
			names[nameKey] = true
			ids[d.ID] = kind
			if wanted[d.Body] {
				return refuse(ErrArchivePathCollision)
			}
			wanted[d.Body] = true
			body, ok := files[d.Body]
			if !ok {
				return refuse(ErrArchiveMissingFile)
			}
			if !validText(body) {
				return refuse(ErrTextEncoding)
			}
			if digestOf(body) != d.SHA256 {
				return refuse(ErrDigestMismatch)
			}
		}
		return nil
	}
	if err := check("team", m.Teams); err != nil {
		return err
	}
	if err := check("persona", m.Personas); err != nil {
		return err
	}
	if err := check("skill", m.Skills); err != nil {
		return err
	}
	for _, d := range m.Teams {
		if err := checkRefs(d.Personas, "persona", ids); err != nil {
			return err
		}
	}
	for _, d := range m.Personas {
		if err := checkRefs(d.Skills, "skill", ids); err != nil {
			return err
		}
		skillIDs := map[string]bool{}
		for _, id := range d.Skills {
			skillIDs[id] = true
		}
		disabled := map[string]bool{}
		for _, id := range d.DisabledSkills {
			if disabled[id] {
				return refuse(ErrDuplicateReference)
			}
			if !skillIDs[id] {
				return refuse(ErrMissingReference)
			}
			disabled[id] = true
		}
	}
	privateScopes := map[string]bool{}
	for _, p := range m.Private {
		if (p.Scope != "global" && p.Scope != "repo" && p.Scope != "place") ||
			(p.Scope == "global" && p.Project != "") ||
			(p.Scope != "global" && p.Project == "") || !validPath(p.Path) ||
			(p.Scope == "repo" && !strings.HasPrefix(p.Project, "project-")) ||
			(p.Scope == "place" && !strings.HasPrefix(p.Project, "place:")) ||
			!strings.HasPrefix(p.Path, "private/") || !digestPattern.MatchString(p.SHA256) {
			return refuse(ErrPrivateScope)
		}
		if privateScopes[privateScopeKey(p)] {
			return refuse(ErrPrivateScope)
		}
		privateScopes[privateScopeKey(p)] = true
		if wanted[p.Path] {
			return refuse(ErrArchivePathCollision)
		}
		wanted[p.Path] = true
		body, ok := files[p.Path]
		if !ok {
			return refuse(ErrArchiveMissingFile)
		}
		if !validText(body) {
			return refuse(ErrTextEncoding)
		}
		if _, err := ParsePrivateDocument(body); err != nil {
			return err
		}
		if digestOf(body) != p.SHA256 {
			return refuse(ErrDigestMismatch)
		}
	}
	for path := range files {
		if !wanted[path] {
			return refuse(ErrArchiveExtraFile)
		}
	}
	return nil
}

func checkRefs(refs []string, kind string, ids map[string]string) error {
	seen := map[string]bool{}
	for _, id := range refs {
		if seen[id] {
			return refuse(ErrDuplicateReference)
		}
		seen[id] = true
		if ids[id] != kind {
			return refuse(ErrMissingReference)
		}
	}
	return nil
}

func validIcon(i Icon) bool {
	if !colorPattern.MatchString(i.Accent) || len(i.Cells) != 7 {
		return false
	}
	for _, row := range i.Cells {
		if len(row) != 8 {
			return false
		}
		for _, cell := range row {
			if cell != nil && !colorPattern.MatchString(*cell) {
				return false
			}
		}
	}
	return true
}

func validText(data []byte) bool {
	return utf8.Valid(data) && !bytes.HasPrefix(data, []byte{0xef, 0xbb, 0xbf}) && !bytes.ContainsRune(data, 0)
}

func digestOf(data []byte) string {
	sum := sha256.Sum256(data)
	return "sha256:" + hex.EncodeToString(sum[:])
}
