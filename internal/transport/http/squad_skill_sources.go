package http

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"io"
	"io/fs"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/sainteye/clawdline/internal/domain/capacity"
)

type skillSource struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	Purpose  string `json:"purpose"`
	Location string `json:"location"`
	Path     string `json:"-"`
}

type skillSourceFile struct {
	Path          string `json:"path"`
	ContentBase64 string `json:"content_base64"`
}

type skillSourceDetail struct {
	Name        string            `json:"name"`
	Purpose     string            `json:"purpose"`
	Content     string            `json:"content"`
	Files       []skillSourceFile `json:"files"`
	FolderError string            `json:"folder_error,omitempty"`
}

type skillSourceRoot struct {
	Path  string
	Label string
}

const maxSkillSourceSummaryRunes = 240

func sourceSummary(value string) string {
	runes := []rune(strings.Join(strings.Fields(value), " "))
	if len(runes) > maxSkillSourceSummaryRunes {
		runes = runes[:maxSkillSourceSummaryRunes]
	}
	return string(runes)
}

func sourceRoots(provider, project, home string) []skillSourceRoot {
	var roots []skillSourceRoot
	add := func(path, label string) {
		if path != "" {
			roots = append(roots, skillSourceRoot{path, label})
		}
	}
	if project != "" {
		switch provider {
		case "project":
			add(filepath.Join(project, ".claude", "skills"), "Project · .claude/skills")
			add(filepath.Join(project, ".agents", "skills"), "Project · .agents/skills")
			add(filepath.Join(project, ".codex", "skills"), "Project · .codex/skills")
		case "claude-code":
			add(filepath.Join(project, ".claude", "skills"), "Project · .claude/skills")
		case "codex":
			add(filepath.Join(project, ".agents", "skills"), "Project · .agents/skills")
			add(filepath.Join(project, ".codex", "skills"), "Project · .codex/skills")
		}
	}
	if home == "" || provider == "project" {
		return roots
	}
	if provider == "claude-code" {
		add(filepath.Join(home, ".claude", "skills"), "Claude Code · 個人技能")
		add(filepath.Join(home, ".claude", "plugins", "cache"), "Claude Code · 擴充技能")
	} else if provider == "codex" {
		add(filepath.Join(home, ".agents", "skills"), "Codex · 個人技能")
		add(filepath.Join(home, ".codex", "skills"), "Codex · 個人技能")
		add(filepath.Join(home, ".codex", "plugins", "cache"), "Codex · 擴充技能")
	}
	return roots
}

func skillSourceID(path string) string {
	sum := sha256.Sum256([]byte(path))
	return hex.EncodeToString(sum[:])
}

func sourceField(content, field string) string {
	content = strings.ReplaceAll(content, "\r\n", "\n")
	if !strings.HasPrefix(content, "---\n") {
		return ""
	}
	end := strings.Index(content[4:], "\n---")
	if end < 0 {
		return ""
	}
	lines := strings.Split(content[4:4+end], "\n")
	for index, line := range lines {
		if strings.HasPrefix(line, field+":") {
			value := strings.TrimSpace(strings.TrimPrefix(line, field+":"))
			if value == ">" || value == "|" || value == ">-" || value == "|-" {
				parts := []string{}
				for _, continued := range lines[index+1:] {
					if continued != "" && continued[0] != ' ' && continued[0] != '\t' {
						break
					}
					if strings.TrimSpace(continued) != "" {
						parts = append(parts, strings.TrimSpace(continued))
					}
				}
				return strings.Join(parts, " ")
			}
			return strings.Trim(value, "\"'")
		}
	}
	return ""
}

func discoverSkillSources(roots []skillSourceRoot, limit int, headBytes int64) []skillSource {
	out := make([]skillSource, 0)
	seen := map[string]bool{}
	for _, root := range roots {
		if len(out) >= limit {
			break
		}
		_ = filepath.WalkDir(root.Path, func(path string, entry fs.DirEntry, err error) error {
			if err != nil {
				return nil
			}
			if entry.IsDir() {
				if path != root.Path && (entry.Name() == ".git" || entry.Name() == "node_modules") {
					return fs.SkipDir
				}
				return nil
			}
			if entry.Name() != "SKILL.md" || !entry.Type().IsRegular() {
				return nil
			}
			if strings.Contains(root.Label, "擴充技能") {
				relative, _ := filepath.Rel(root.Path, path)
				if !strings.Contains("/"+filepath.ToSlash(relative), "/skills/") {
					return nil
				}
			}
			if seen[path] {
				return nil
			}
			seen[path] = true
			file, err := os.Open(path)
			if err != nil {
				return nil
			}
			body, readErr := io.ReadAll(io.LimitReader(file, headBytes))
			_ = file.Close()
			if readErr != nil {
				return nil
			}
			relative, _ := filepath.Rel(root.Path, filepath.Dir(path))
			name := sourceField(string(body), "name")
			if name == "" {
				name = filepath.Base(filepath.Dir(path))
			}
			name = sourceSummary(name)
			out = append(out, skillSource{ID: skillSourceID(path), Name: name,
				Purpose: sourceSummary(sourceField(string(body), "description")), Location: root.Label + " / " + filepath.ToSlash(relative), Path: path})
			if len(out) >= limit {
				return fs.SkipAll
			}
			return nil
		})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Location < out[j].Location })
	return out
}

func readSkillSource(source skillSource, folder bool, limit int64) (skillSourceDetail, error) {
	root := filepath.Dir(source.Path)
	mainFile, err := os.Open(source.Path)
	if err != nil {
		return skillSourceDetail{}, err
	}
	main, err := io.ReadAll(io.LimitReader(mainFile, limit+1))
	_ = mainFile.Close()
	if err != nil {
		return skillSourceDetail{}, err
	}
	if len(main) == 0 || int64(len(main)) > limit {
		return skillSourceDetail{}, errors.New("skill_text_too_large")
	}
	out := skillSourceDetail{Name: source.Name, Purpose: source.Purpose, Content: string(main), Files: []skillSourceFile{}}
	if !folder {
		return out, nil
	}
	remaining := limit - int64(len(main))
	err = filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if path == root {
			return nil
		}
		if entry.IsDir() {
			if entry.Name() == ".git" || entry.Name() == "node_modules" {
				return fs.SkipDir
			}
			return nil
		}
		if path == source.Path {
			return nil
		}
		if !entry.Type().IsRegular() {
			return nil
		}
		relative, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		if info.Size() > remaining {
			return errors.New("skill_folder_too_large")
		}
		file, err := os.Open(path)
		if err != nil {
			return err
		}
		bytes, err := io.ReadAll(io.LimitReader(file, remaining+1))
		_ = file.Close()
		if err != nil {
			return err
		}
		if int64(len(bytes)) > remaining {
			return errors.New("skill_folder_too_large")
		}
		remaining -= int64(len(bytes))
		out.Files = append(out.Files, skillSourceFile{Path: filepath.ToSlash(relative), ContentBase64: base64.StdEncoding.EncodeToString(bytes)})
		return nil
	})
	if err != nil {
		out.Files = []skillSourceFile{}
		out.FolderError = "技能資料夾無法完整複製；可以改選只複製 SKILL.md 文字。"
	}
	return out, nil
}

func (s *Server) squadSkillSources(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeRefusal(w, http.StatusMethodNotAllowed, "method_not_allowed", "Use GET.")
		return
	}
	q := r.URL.Query()
	for key := range q {
		if key != "provider" && key != "place_id" && key != "id" && key != "folder" || len(q[key]) != 1 {
			writeRefusal(w, http.StatusBadRequest, "bad_request", "Unsupported skill source query.")
			return
		}
	}
	provider := q.Get("provider")
	if provider != "project" && provider != "claude-code" && provider != "codex" {
		writeRefusal(w, http.StatusBadRequest, "invalid_source", "Choose a known skill source.")
		return
	}
	if q.Has("place_id") && q.Get("place_id") == "" || q.Has("id") && q.Get("id") == "" {
		writeRefusal(w, http.StatusBadRequest, "bad_request", "Choose a listed Project and skill.")
		return
	}
	project := ""
	if placeID := q.Get("place_id"); placeID != "" {
		place, ok := s.squadKnownPlace(r.Context(), placeID)
		if !ok {
			writeRefusal(w, http.StatusNotFound, "unknown_project", "That Project is not known to this machine.")
			return
		}
		project = place.Key
	} else if provider == "project" {
		writeRefusal(w, http.StatusBadRequest, "project_required", "Choose a Project before browsing its skills.")
		return
	}
	if q.Has("folder") && (!q.Has("id") || q.Get("folder") != "true" && q.Get("folder") != "false") {
		writeRefusal(w, http.StatusBadRequest, "bad_request", "Choose a listed skill and import type.")
		return
	}
	if q.Has("id") && !q.Has("folder") {
		writeRefusal(w, http.StatusBadRequest, "bad_request", "Choose an import type.")
		return
	}
	if q.Has("id") && !squadWriteAllowed(r) {
		writeRefusal(w, http.StatusForbidden, "forbidden", "Skill content needs a write-capable paired device.")
		return
	}
	home, _ := os.UserHomeDir()
	rows := discoverSkillSources(sourceRoots(provider, project, home), int(CapacityLimit(capacity.SquadEntities)), CapacityLimit(capacity.SquadBodyBytes))
	if id := q.Get("id"); id != "" {
		if len(id) != 64 {
			writeRefusal(w, http.StatusBadRequest, "invalid_source", "Choose a listed skill.")
			return
		}
		for _, row := range rows {
			if row.ID == id {
				detail, err := readSkillSource(row, q.Get("folder") == "true", CapacityLimit(capacity.SquadBodyBytes))
				if err != nil {
					writeRefusal(w, http.StatusConflict, "skill_source_changed", "The skill changed or its text is too large; refresh the list.")
					return
				}
				writeJSON(w, detail)
				return
			}
		}
		writeRefusal(w, http.StatusNotFound, "skill_source_missing", "The skill is no longer in the selected source; refresh the list.")
		return
	}
	writeJSON(w, map[string]any{"skills": rows})
}
