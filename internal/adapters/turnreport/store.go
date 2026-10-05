package turnreport

import (
	"bytes"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
)

// Where a report is kept, so the daemon on this machine can answer it at
// http://127.0.0.1:<port>/reports/<id> as well as the browser opening its
// file:// address.
//
// A web page cannot open a file:// address, and the console is a web page:
// its Markdown shows a local path as grey text on purpose. So a report also
// gets an address the console can make a link of. The daemon serves only
// what this package wrote here, by an id it never builds a path from beyond
// one directory name it has checked; see internal/transport/http/reports.go
// for who may ask.
//
// Every report is one directory, <state dir>/reports/<date>-<32 hex>/, with
// one page in it, report.html. The date keeps the directory readable; the
// 128 random bits keep the address from being guessed.

// PageName is the one file a report directory holds.
const PageName = "report.html"

// generatorMarker is in the head of every page this package writes. A file
// without it is not a report, whatever its name, and the daemon does not
// serve it.
const generatorMarker = `<meta name="generator" content="clawdline report">`

// servedLimit is the largest page the daemon answers: the report's own
// reportLimit of text, its JSON escaping, and the page around it.
const servedLimit = 48 << 20

var idPattern = regexp.MustCompile(`^[0-9]{4}-[0-9]{2}-[0-9]{2}-[0-9a-f]{32}$`)

// ErrNoReport is an id that names no report this machine wrote.
var ErrNoReport = errors.New("no such report")

// ReportsDir is where reports are kept under a state directory.
func ReportsDir(stateDir string) string { return filepath.Join(stateDir, "reports") }

// NewID is a fresh report id for date ("2006-01-02").
func NewID(date string) (string, error) {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", err
	}
	id := date + "-" + hex.EncodeToString(b[:])
	if !ValidID(id) {
		return "", fmt.Errorf("%q is not a date", date)
	}
	return id, nil
}

// ValidID is whether id has the one shape NewID makes. Nothing else is ever
// joined to a path.
func ValidID(id string) bool { return idPattern.MatchString(id) }

// ReadStored answers the page a report id names, or ErrNoReport. It refuses
// an id of any other shape, a directory or page that is a link or not what
// it should be, a page larger than servedLimit, and a page this package did
// not write.
func ReadStored(stateDir, id string) ([]byte, error) {
	if !ValidID(id) {
		return nil, ErrNoReport
	}
	dir := filepath.Join(ReportsDir(stateDir), id)
	if fi, err := os.Lstat(dir); err != nil || !fi.IsDir() {
		return nil, ErrNoReport
	}
	page := filepath.Join(dir, PageName)
	fi, err := os.Lstat(page)
	if err != nil || !fi.Mode().IsRegular() || fi.Size() > servedLimit {
		return nil, ErrNoReport
	}
	f, err := os.Open(page)
	if err != nil {
		return nil, ErrNoReport
	}
	defer f.Close()
	// The file opened must be the one looked at: a link swapped in between
	// is a different file.
	if opened, err := f.Stat(); err != nil || !os.SameFile(fi, opened) {
		return nil, ErrNoReport
	}
	data, err := io.ReadAll(io.LimitReader(f, servedLimit+1))
	if err != nil || len(data) > servedLimit {
		return nil, ErrNoReport
	}
	head := data
	if len(head) > 4096 {
		head = head[:4096]
	}
	if !bytes.Contains(head, []byte(generatorMarker)) {
		return nil, ErrNoReport
	}
	return data, nil
}
