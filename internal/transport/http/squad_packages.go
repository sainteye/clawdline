package http

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net/http"

	"github.com/sainteye/clawdline/internal/adapters/projects"
	"github.com/sainteye/clawdline/internal/adapters/store"
	"github.com/sainteye/clawdline/internal/app/squadpackages"
	"github.com/sainteye/clawdline/internal/domain/capacity"
	"github.com/sainteye/clawdline/internal/domain/cloud"
	"github.com/sainteye/clawdline/internal/domain/squadpack"
)

func packageBody(w http.ResponseWriter, r *http.Request, out any) bool {
	raw, err := io.ReadAll(io.LimitReader(r.Body, CapacityLimit(capacity.SquadPackageRequestBytes)+1))
	if err != nil {
		writeRefusal(w, http.StatusBadRequest, "bad_request", "The package request could not be read.")
		return false
	}
	if int64(len(raw)) > CapacityLimit(capacity.SquadPackageRequestBytes) {
		writeRefusal(w, http.StatusRequestEntityTooLarge, "body_too_large", "The package request is too large.")
		return false
	}
	if _, err := cloud.Parse(raw); err != nil {
		writeRefusal(w, http.StatusBadRequest, "bad_request", "The package request must be one JSON object without duplicate keys.")
		return false
	}
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	if err := dec.Decode(out); err != nil || dec.Decode(new(any)) != io.EOF {
		writeRefusal(w, http.StatusBadRequest, "bad_request", "The package request has an unexpected shape.")
		return false
	}
	return true
}

func packageArchive(encoded string) ([]byte, error) {
	if len(encoded) > base64.StdEncoding.EncodedLen(squadpack.MaxArchiveBytes) {
		return nil, &squadpack.Refusal{Code: squadpack.ErrArchiveTooLarge}
	}
	archive, err := base64.StdEncoding.Strict().DecodeString(encoded)
	if err != nil {
		return nil, &squadpack.Refusal{Code: squadpack.ErrArchiveInvalid}
	}
	return archive, nil
}

func (s *Server) packageService() squadpackages.Service {
	return squadpackages.Service{Catalog: store.PackageCatalog{Store: s.store,
		Limits: squadLimits(), PreviewRows: CapacityLimit(capacity.SquadPackagePreviewRows)}}
}

func (s *Server) packageScopeKnown(ctx context.Context, id string) (bool, error) {
	if id == "global" {
		return true, nil
	}
	for _, p := range s.squadListedPlaces(ctx) {
		scope, ok := projects.ResolveScope(p.Path)
		if ok && scope.ID == id {
			return true, nil
		}
	}
	saved, err := s.store.SquadScopes(ctx)
	if err != nil {
		return false, err
	}
	for _, scope := range saved {
		if scope == id {
			return true, nil
		}
	}
	return false, nil
}

func (s *Server) packageScopesKnown(ctx context.Context, ids []string) (bool, error) {
	for _, id := range ids {
		known, err := s.packageScopeKnown(ctx, id)
		if err != nil || !known {
			return false, err
		}
	}
	return true, nil
}

func (s *Server) packageCheckScopes(w http.ResponseWriter, ctx context.Context, ids []string) bool {
	known, err := s.packageScopesKnown(ctx, ids)
	if err != nil {
		squadFailure(w, err, true)
		return false
	}
	if !known {
		writeRefusal(w, http.StatusNotFound, "unknown_project", "That scope is not known to this machine.")
		return false
	}
	return true
}

func packageFailure(w http.ResponseWriter, err error) {
	var refusal *squadpack.Refusal
	var conflict store.SquadVersionConflict
	switch {
	case errors.As(err, &refusal):
		status := http.StatusBadRequest
		switch refusal.Code {
		case squadpack.ErrArchiveTooLarge, squadpack.ErrArchiveEntryCount,
			squadpack.ErrArchiveExpansion, squadpack.ErrArchiveFileTooLarge, squadpack.ErrManifestTooLarge:
			status = http.StatusRequestEntityTooLarge
		case "preview_expired", "settings_changed", squadpack.ErrReferenceConflict,
			squadpack.ErrChoiceInvalid, squadpack.ErrNameConflict:
			status = http.StatusConflict
		}
		writeRefusal(w, status, refusal.Code, "The offline package could not be accepted.")
	case errors.As(err, &conflict), errors.Is(err, store.ErrSquadReceiptConflict),
		errors.Is(err, store.ErrSquadVersionContent), errors.Is(err, store.ErrSquadCapacityFull):
		squadFailure(w, err, false)
	default:
		squadFailure(w, err, false)
	}
}

func (s *Server) squadPackagesRoute(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeRefusal(w, http.StatusMethodNotAllowed, "method_not_allowed", "Use POST.")
		return
	}
	switch routePath(r) {
	case "/v1/squad-packages/preview":
		s.squadPackagePreview(w, r)
	case "/v1/squad-packages/adopt":
		s.squadPackageAdopt(w, r)
	case "/v1/squad-packages/export":
		s.squadPackageExport(w, r)
	default:
		writeRefusal(w, http.StatusNotFound, "not_found", "No such package route.")
	}
}

func (s *Server) squadPackagePreview(w http.ResponseWriter, r *http.Request) {
	var req struct {
		ArchiveBase64 string `json:"archive_base64"`
		ScopeID       string `json:"scope_id"`
	}
	if !packageBody(w, r, &req) {
		return
	}
	if !s.packageCheckScopes(w, r.Context(), []string{req.ScopeID}) {
		return
	}
	archive, err := packageArchive(req.ArchiveBase64)
	if err != nil {
		packageFailure(w, err)
		return
	}
	result, err := s.packageService().Preview(context.WithoutCancel(r.Context()),
		personPrincipal(r), req.ScopeID, archive)
	if err != nil {
		packageFailure(w, err)
		return
	}
	writeJSON(w, result)
}

func (s *Server) squadPackageAdopt(w http.ResponseWriter, r *http.Request) {
	key, ok := squadWriteDoor(w, r)
	if !ok {
		return
	}
	var req struct {
		ArchiveBase64  string            `json:"archive_base64"`
		ArchiveDigest  string            `json:"archive_digest"`
		PreviewDigest  string            `json:"preview_digest"`
		PreviewToken   string            `json:"preview_token"`
		CatalogVersion int64             `json:"catalog_version"`
		ScopeID        string            `json:"scope_id"`
		Choices        map[string]string `json:"choices"`
		PrivateScopes  []string          `json:"private_scopes"`
		ConfirmPrivate bool              `json:"confirm_private"`
	}
	if !packageBody(w, r, &req) {
		return
	}
	if !s.packageCheckScopes(w, r.Context(), append([]string{req.ScopeID}, req.PrivateScopes...)) {
		return
	}
	archive, err := packageArchive(req.ArchiveBase64)
	if err != nil {
		packageFailure(w, err)
		return
	}
	receipt, err := s.packageService().Adopt(context.WithoutCancel(r.Context()),
		squadpackages.AdoptInput{Actor: personPrincipal(r), Scope: req.ScopeID,
			RequestID: key, PreviewToken: req.PreviewToken, ArchiveDigest: req.ArchiveDigest,
			PreviewDigest: req.PreviewDigest, ExpectedVersion: req.CatalogVersion,
			Choices: req.Choices, PrivateScopes: req.PrivateScopes,
			ConfirmPrivate: req.ConfirmPrivate, Archive: archive})
	if err != nil {
		packageFailure(w, err)
		return
	}
	writeJSON(w, receipt)
}

func (s *Server) squadPackageExport(w http.ResponseWriter, r *http.Request) {
	var req struct {
		PrivateScopes  []string `json:"private_scopes"`
		ConfirmPrivate bool     `json:"confirm_private"`
	}
	if !packageBody(w, r, &req) {
		return
	}
	if len(req.PrivateScopes) > 0 && !squadWriteAllowed(r) {
		writeRefusal(w, http.StatusForbidden, "forbidden", "Private export needs a write-capable person.")
		return
	}
	if !s.packageCheckScopes(w, r.Context(), req.PrivateScopes) {
		return
	}
	archive, err := s.packageService().Export(r.Context(), req.PrivateScopes, req.ConfirmPrivate)
	if err != nil {
		packageFailure(w, err)
		return
	}
	h := sha256.Sum256(archive)
	writeJSON(w, struct {
		ArchiveBase64 string `json:"archive_base64"`
		ArchiveDigest string `json:"archive_digest"`
		FileName      string `json:"file_name"`
		MIMEType      string `json:"mime_type"`
	}{base64.StdEncoding.EncodeToString(archive), "sha256:" + hex.EncodeToString(h[:]),
		"clawdline-squads.zip", "application/zip"})
}
