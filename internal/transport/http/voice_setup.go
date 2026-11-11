package http

import (
	"context"
	"errors"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/sainteye/clawdline/internal/adapters/install"
	"github.com/sainteye/clawdline/internal/adapters/whisper"
)

var voiceModelInstall = whisper.InstallDefaultModel

// voiceSetupRoute is the Settings tab's local installation surface. Reading
// it performs no network request. POST downloads only the one pinned model;
// installing an executable remains an explicit terminal command because most
// Linux distributions require a compiler or administrator authority.
func (s *Server) voiceSetupRoute(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet, http.MethodHead:
		reply := voiceSetup(s.whisper())
		reply.PolishAssistants = voicePolishAvailable()
		reply.VocabularyBuiltin = append([]string{}, whisper.AlwaysExpected...)
		reply.VocabularyProjects = s.voiceProjectTerms()
		if reply.VocabularyProjects == nil {
			reply.VocabularyProjects = []string{}
		}
		writeJSON(w, reply)
	case http.MethodPost:
		path, err := voiceModelInstall(r.Context(), http.DefaultClient, "")
		if err != nil {
			switch {
			case errors.Is(err, whisper.ErrInstallBusy):
				writeRefusal(w, http.StatusConflict, "voice_model_busy", "A Whisper model is already being downloaded.")
			case errors.Is(err, context.Canceled):
				writeRefusal(w, http.StatusRequestTimeout, "voice_model_abandoned", "The model download was abandoned before it finished.")
			default:
				writeRefusal(w, http.StatusBadGateway, "voice_model_install_failed", "The Whisper model could not be downloaded and verified: "+err.Error())
			}
			return
		}
		writeJSON(w, voiceModelInstallReply{OK: true, Model: filepath.Base(path)})
	default:
		writeRefusal(w, http.StatusMethodNotAllowed, "method_not_allowed", "GET or POST")
	}
}

type voiceSetupReply struct {
	Platform      string   `json:"platform"`
	Status        string   `json:"status"`
	Binary        string   `json:"binary"`
	Model         string   `json:"model"`
	BinaryDirs    []string `json:"binary_dirs"`
	ModelDirs     []string `json:"model_dirs"`
	BinaryInstall string   `json:"binary_install"`
	ModelName     string   `json:"model_name"`
	ModelBytes    int64    `json:"model_bytes"`
	// Cleanup and vocabulary, for the voice tab (docs/voice.md).
	PolishAssistants   []string `json:"polish_assistants"`
	VocabularyBuiltin  []string `json:"vocabulary_builtin"`
	VocabularyProjects []string `json:"vocabulary_projects"`
}

type voiceModelInstallReply struct {
	OK    bool   `json:"ok"`
	Model string `json:"model"`
}

func voiceSetup(t whisper.Transcriber) voiceSetupReply {
	binary, model := t.Found()
	status := "ready"
	if binary == "" {
		status = "no_binary"
	} else if model == "" {
		status = "no_model"
	}
	binaryDirs, modelDirs := t.SearchPaths()
	pm := install.DetectPackageManager(runtime.GOOS, func(name string) bool {
		_, err := exec.LookPath(name)
		return err == nil
	})
	return voiceSetupReply{
		Platform:      runtime.GOOS,
		Status:        status,
		Binary:        installedName(binary),
		Model:         installedName(model),
		BinaryDirs:    privatePaths(binaryDirs),
		ModelDirs:     privatePaths(modelDirs),
		BinaryInstall: whisper.BinaryInstallCommand(runtime.GOOS, pm),
		ModelName:     whisper.DefaultModel.Name,
		ModelBytes:    whisper.DefaultModel.Size,
	}
}

func installedName(path string) string {
	if path == "" {
		return ""
	}
	return filepath.Base(path)
}

func privatePaths(paths []string) []string {
	home, _ := os.UserHomeDir()
	out := make([]string, 0, len(paths))
	for _, path := range paths {
		if home != "" && (path == home || strings.HasPrefix(path, home+string(filepath.Separator))) {
			path = "~" + strings.TrimPrefix(path, home)
		}
		out = append(out, path)
	}
	return out
}
