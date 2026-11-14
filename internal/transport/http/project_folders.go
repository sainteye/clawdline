package http

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/sainteye/clawdline/internal/adapters/store"
)

// maxProjectFolderRows bounds one browser directory page. The browser can enter
// a path directly when a directory has more entries than this page can show.
const maxProjectFolderRows = 128

// maxProjectFolderSearchRows bounds one folder-name search answer, and
// maxProjectFolderSearchReads the directories one search may read: home and
// its children, two levels in all. A longer query narrows a truncated answer.
const (
	maxProjectFolderSearchRows  = 50
	maxProjectFolderSearchReads = 200
	maxProjectFolderQueryRunes  = 64
)

// macProtectedFolders are the home folders macOS guards with a permission
// prompt, plus Library. A search does not enter them: the prompt would name
// Clawdline for a read nobody asked for, and the Mac app's own folder panel
// reaches them with the person's choice instead.
var macProtectedFolders = map[string]bool{
	"Applications": true, "Desktop": true, "Documents": true, "Downloads": true,
	"Library": true, "Movies": true, "Music": true, "Pictures": true, "Public": true,
}

var projectFolderSearchSkipsProtected = runtime.GOOS == "darwin"

type projectFolder struct {
	Name string `json:"name"`
	Path string `json:"path"`
}

// projectFoldersRoute lets a paired viewer choose a directory on the selected
// machine. It sends names only, never file contents, and skips hidden entries
// and links. Registration still validates the chosen path independently.
func (s *Server) projectFoldersRoute(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodPost {
		s.projectFolderCreateRoute(w, r)
		return
	}
	if r.Method != http.MethodGet {
		writeRefusal(w, http.StatusMethodNotAllowed, "method_not_allowed", "GET only")
		return
	}
	for key := range r.URL.Query() {
		if key != "path" && key != "q" {
			writeRefusal(w, http.StatusBadRequest, "bad_request", "Only path or q is accepted.")
			return
		}
	}
	if r.URL.Query().Has("q") {
		if r.URL.Query().Has("path") {
			writeRefusal(w, http.StatusBadRequest, "bad_request", "Send path or q, not both.")
			return
		}
		projectFolderSearch(w, r.URL.Query().Get("q"))
		return
	}
	dir := r.URL.Query().Get("path")
	if dir == "" {
		var err error
		dir, err = os.UserHomeDir()
		if err != nil {
			writeRefusal(w, http.StatusServiceUnavailable, "home_unavailable", "The home directory is unavailable.")
			return
		}
	}
	if !validProjectFolderPath(dir) {
		writeRefusal(w, http.StatusBadRequest, "invalid_project_path", "Choose an absolute directory on this machine.")
		return
	}
	dir = filepath.Clean(dir)
	info, err := os.Stat(dir)
	if err != nil || !info.IsDir() {
		writeRefusal(w, http.StatusNotFound, "project_folder_unavailable", "This directory is unavailable on the selected machine.")
		return
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		writeRefusal(w, http.StatusForbidden, "project_folder_unreadable", "This directory cannot be read on the selected machine.")
		return
	}
	rows := make([]projectFolder, 0)
	truncated := false
	for _, entry := range entries {
		if strings.HasPrefix(entry.Name(), ".") || !entry.IsDir() {
			continue
		}
		if len(rows) == int(CapacityLimit("projects.folder_rows")) {
			truncated = true
			break
		}
		rows = append(rows, projectFolder{Name: entry.Name(), Path: filepath.Join(dir, entry.Name())})
	}
	parent := filepath.Dir(dir)
	if parent == dir {
		parent = ""
	}
	writeJSON(w, struct {
		Path      string          `json:"path"`
		Parent    string          `json:"parent,omitempty"`
		Folders   []projectFolder `json:"folders"`
		Truncated bool            `json:"truncated"`
	}{dir, parent, rows, truncated})
}

// projectFolderSearch finds folders whose names contain the query, up to two
// levels under the home directory, so a person can type a Project's name
// instead of walking to it. Names only, hidden entries and links skipped, as
// in the listing above.
func projectFolderSearch(w http.ResponseWriter, raw string) {
	query := strings.TrimSpace(raw)
	if query == "" || utf8.RuneCountInString(query) > maxProjectFolderQueryRunes || strings.IndexByte(query, 0) >= 0 {
		writeRefusal(w, http.StatusBadRequest, "bad_request", "Type one to 64 characters of a folder name.")
		return
	}
	home, err := os.UserHomeDir()
	if err != nil {
		writeRefusal(w, http.StatusServiceUnavailable, "home_unavailable", "The home directory is unavailable.")
		return
	}
	top, err := os.ReadDir(home)
	if err != nil {
		writeRefusal(w, http.StatusForbidden, "project_folder_unreadable", "This directory cannot be read on the selected machine.")
		return
	}
	needle := strings.ToLower(query)
	rowLimit := int(CapacityLimit("projects.folder_search_rows"))
	readLimit := int(CapacityLimit("projects.folder_search_reads"))
	rows := make([]projectFolder, 0)
	truncated, skipped := false, false
	// take records a match and answers false once the answer is full.
	take := func(dir string, entry os.DirEntry) bool {
		if strings.HasPrefix(entry.Name(), ".") || !entry.IsDir() || !strings.Contains(strings.ToLower(entry.Name()), needle) {
			return true
		}
		if len(rows) == rowLimit {
			truncated = true
			return false
		}
		rows = append(rows, projectFolder{Name: entry.Name(), Path: filepath.Join(dir, entry.Name())})
		return true
	}
	children := make([]string, 0, len(top))
	for _, entry := range top {
		if !take(home, entry) {
			break
		}
		if strings.HasPrefix(entry.Name(), ".") || !entry.IsDir() {
			continue
		}
		if projectFolderSearchSkipsProtected && macProtectedFolders[entry.Name()] {
			skipped = true
			continue
		}
		children = append(children, filepath.Join(home, entry.Name()))
	}
	reads := 1
	for _, dir := range children {
		if truncated {
			break
		}
		if reads == readLimit {
			truncated = true
			break
		}
		reads++
		entries, err := os.ReadDir(dir)
		if err != nil {
			continue
		}
		for _, entry := range entries {
			if !take(dir, entry) {
				break
			}
		}
	}
	writeJSON(w, struct {
		Query            string          `json:"query"`
		Folders          []projectFolder `json:"folders"`
		Truncated        bool            `json:"truncated"`
		ProtectedSkipped bool            `json:"protected_skipped"`
	}{query, rows, truncated, skipped})
}

func validProjectFolderPath(path string) bool {
	return filepath.IsAbs(path) && len(path) <= 4096 && strings.IndexByte(path, 0) < 0
}

// Create exactly one child of an existing directory. A receipt preserves the
// outcome across lost replies; an existing file, folder or link is never reused.
func (s *Server) projectFolderCreateRoute(w http.ResponseWriter, r *http.Request) {
	if !maySend(r) {
		writeRefusal(w, 403, "forbidden", "This device may browse folders but cannot create one.")
		return
	}
	if r.URL.RawQuery != "" {
		writeRefusal(w, 400, "bad_request", "Folder creation takes no query fields.")
		return
	}
	key, ok := idempotencyKey(w, r)
	if !ok {
		return
	}
	if key == "" {
		writeRefusal(w, 400, "idempotency_key_required", "Creating a folder needs an Idempotency-Key.")
		return
	}
	// boundBodies has already applied the registered command body limit.
	body, err := io.ReadAll(r.Body)
	var input struct {
		Parent string `json:"parent"`
		Name   string `json:"name"`
	}
	dec := json.NewDecoder(bytes.NewReader(body))
	dec.DisallowUnknownFields()
	if err != nil || dec.Decode(&input) != nil || dec.Decode(new(any)) != io.EOF {
		writeRefusal(w, 400, "bad_request", "Supply the parent directory and one folder name.")
		return
	}
	if !validProjectFolderPath(input.Parent) {
		writeRefusal(w, 400, "invalid_project_path", "Choose an absolute directory on this machine.")
		return
	}
	if strings.TrimSpace(input.Name) == "" || input.Name == "." || input.Name == ".." || !filepath.IsLocal(input.Name) ||
		strings.ContainsAny(input.Name, `/\`) || strings.IndexFunc(input.Name, unicode.IsControl) >= 0 {
		writeRefusal(w, 400, "invalid_folder_name", "Enter one folder name without path separators or control characters.")
		return
	}
	path := filepath.Join(input.Parent, input.Name)
	if !validProjectFolderPath(path) || filepath.Dir(path) != filepath.Clean(input.Parent) {
		writeRefusal(w, 400, "invalid_project_path", "Choose an absolute directory on this machine.")
		return
	}
	s.receipted(w, r, store.ReceiptKey{Scope: scopePlaces, Actor: actorOf(r), Key: key},
		requestDigest([]byte(r.Method), []byte(routePath(r)), body),
		func(status int) bool { return status != http.StatusTooManyRequests }, func(w http.ResponseWriter) {
			root, err := os.OpenRoot(input.Parent)
			if err != nil {
				writeRefusal(w, 404, "project_folder_unavailable", "This directory is unavailable on the selected machine.")
				return
			}
			defer root.Close()
			if err := root.Mkdir(input.Name, 0o755); err != nil {
				switch {
				case errors.Is(err, os.ErrExist):
					writeRefusal(w, 409, "project_folder_exists", "A file or folder with this name already exists. Choose another name.")
				case errors.Is(err, os.ErrPermission):
					writeRefusal(w, 403, "project_folder_unwritable", "This directory does not allow creating a folder with this machine's permissions.")
				default:
					writeRefusal(w, 500, "project_folder_create_failed", "The folder could not be created. Check the name and the parent directory.")
				}
				return
			}
			writeJSON(w, projectFolder{Name: input.Name, Path: path})
		})
}
