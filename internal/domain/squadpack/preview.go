package squadpack

import (
	"encoding/json"
	"sort"
	"strconv"
	"strings"
)

// Existing is the active catalog projection needed to preview an archive.
// References contain stable IDs; the catalog adapter keeps immutable version
// rows available to old snapshots separately.
type Existing struct {
	Kind       string
	ID         string
	Version    string
	Digest     string
	Name       string
	Builtin    bool
	References []string
}

type Change struct {
	Kind            string   `json:"kind"`
	ID              string   `json:"id"`
	Version         string   `json:"version"`
	Action          string   `json:"action"` // add, update, unchanged, conflict
	ConflictCode    string   `json:"conflict_code,omitempty"`
	PreviousVersion string   `json:"previous_version,omitempty"`
	Dependents      []string `json:"dependents"`
}

type Preview struct {
	ArchiveDigest  string   `json:"archive_digest"`
	CatalogVersion int64    `json:"catalog_version"`
	Scope          string   `json:"scope"`
	Source         string   `json:"source"`
	License        string   `json:"license"`
	PrivateScopes  []string `json:"private_scopes"`
	Changes        []Change `json:"changes"`
	Digest         string   `json:"preview_digest"`
}

const (
	ErrCatalogInvalid    = "catalog_invalid"
	ErrChoiceInvalid     = "choice_invalid"
	ErrReferenceConflict = "reference_conflict"
)

// Analyze uses one catalog snapshot. Adoption calls it again inside the
// catalog transaction, after checking the persisted preview credential.
func Analyze(pkg Package, catalogVersion int64, scope string, existing []Existing) (Preview, error) {
	if pkg.Digest == "" || catalogVersion < 0 || scope == "" {
		return Preview{}, refuse(ErrCatalogInvalid)
	}
	byID := map[string]Existing{}
	byName := map[string]string{}
	dependents := map[string][]string{}
	for _, old := range existing {
		key := old.Kind + "\x00" + old.ID
		if old.ID == "" || old.Kind == "" || byID[key].ID != "" {
			return Preview{}, refuse(ErrCatalogInvalid)
		}
		byID[key] = old
		byName[old.Kind+"\x00"+strings.ToLower(old.Name)] = old.ID
		for _, ref := range old.References {
			dependents[ref] = append(dependents[ref], old.ID)
		}
	}
	out := Preview{ArchiveDigest: pkg.Digest, CatalogVersion: catalogVersion, Scope: scope,
		Source: pkg.Manifest.Source, License: pkg.Manifest.License,
		PrivateScopes: []string{}, Changes: []Change{}}
	for _, entry := range pkg.Manifest.Private {
		out.PrivateScopes = append(out.PrivateScopes, privateScopeKey(entry))
	}
	sort.Strings(out.PrivateScopes)
	appendChanges := func(kind string, defs []Definition) {
		for _, d := range defs {
			change := Change{Kind: kind, ID: d.ID, Version: d.Version, Action: "add", Dependents: append([]string{}, dependents[d.ID]...)}
			sort.Strings(change.Dependents)
			if old, ok := byID[kind+"\x00"+d.ID]; ok {
				change.PreviousVersion = old.Version
				switch {
				case old.Builtin:
					change.Action, change.ConflictCode = "conflict", ErrReservedID
				case d.Version == old.Version && definitionDigest(d) == old.Digest:
					change.Action = "unchanged"
				case d.Version == old.Version:
					change.Action, change.ConflictCode = "conflict", "version_content_conflict"
				case compareVersion(d.Version, old.Version) > 0:
					change.Action = "update"
				case compareVersion(d.Version, old.Version) < 0:
					change.Action, change.ConflictCode = "conflict", "version_downgrade"
				default:
					change.Action, change.ConflictCode = "conflict", "version_incomparable"
				}
			}
			if other := byName[kind+"\x00"+strings.ToLower(d.Name)]; other != "" && other != d.ID {
				change.Action, change.ConflictCode = "conflict", ErrNameConflict
			}
			out.Changes = append(out.Changes, change)
		}
	}
	appendChanges("team", pkg.Manifest.Teams)
	appendChanges("persona", pkg.Manifest.Personas)
	appendChanges("skill", pkg.Manifest.Skills)
	b, _ := json.Marshal(struct {
		ArchiveDigest string
		Version       int64
		Scope         string
		Source        string
		License       string
		PrivateScopes []string
		Changes       []Change
	}{out.ArchiveDigest, out.CatalogVersion, out.Scope, out.Source,
		out.License, out.PrivateScopes, out.Changes})
	out.Digest = digestOf(b)
	return out, nil
}

func definitionDigest(d Definition) string {
	b, _ := json.Marshal(d)
	return digestOf(b)
}

// DefinitionDigest binds every manifest field of a shareable definition.
// Store adapters use it for immutable package version rows and previews.
func DefinitionDigest(d Definition) string { return definitionDigest(d) }

func compareVersion(a, b string) int {
	as, bs := strings.Split(a, "."), strings.Split(b, ".")
	if len(as) != 3 || len(bs) != 3 {
		return 0
	}
	for i := range as {
		an, ae := strconv.ParseUint(as[i], 10, 64)
		bn, be := strconv.ParseUint(bs[i], 10, 64)
		if ae != nil || be != nil {
			return 0
		}
		if an < bn {
			return -1
		}
		if an > bn {
			return 1
		}
	}
	return 0
}

// ValidateChoices admits exactly one explicit keep choice for each conflict.
// A skipped dependency is valid only when a matching active ID remains.
func ValidateChoices(pkg Package, preview Preview, choices map[string]string, existing []Existing) error {
	active := map[string]bool{}
	for _, old := range existing {
		active[old.Kind+"\x00"+old.ID] = true
	}
	for _, change := range preview.Changes {
		choice, supplied := choices[change.ID]
		if change.Action == "conflict" {
			if !supplied || choice != "keep" {
				return refuse(ErrChoiceInvalid)
			}
		} else if supplied {
			return refuse(ErrChoiceInvalid)
		}
		if change.Action != "conflict" {
			active[change.Kind+"\x00"+change.ID] = true
		}
	}
	if len(choices) != countConflicts(preview.Changes) {
		return refuse(ErrChoiceInvalid)
	}
	for _, d := range pkg.Manifest.Teams {
		if choices[d.ID] == "keep" {
			continue
		}
		for _, id := range d.Personas {
			if !active["persona\x00"+id] {
				return refuse(ErrReferenceConflict)
			}
		}
	}
	for _, d := range pkg.Manifest.Personas {
		if choices[d.ID] == "keep" {
			continue
		}
		for _, id := range d.Skills {
			if !active["skill\x00"+id] {
				return refuse(ErrReferenceConflict)
			}
		}
	}
	return nil
}

func countConflicts(changes []Change) int {
	n := 0
	for _, c := range changes {
		if c.Action == "conflict" {
			n++
		}
	}
	return n
}
