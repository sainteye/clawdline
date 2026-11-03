package app

import (
	"context"
	"errors"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/sainteye/clawdline/internal/adapters/opsdocs"
	"github.com/sainteye/clawdline/internal/adapters/projectfiles"
	"github.com/sainteye/clawdline/internal/adapters/store"
	"github.com/sainteye/clawdline/internal/contract"
	domain "github.com/sainteye/clawdline/internal/domain/opsdocs"
	"github.com/sainteye/clawdline/internal/domain/work"
)

// OpsDocuments is a Project's list of operational documents: files beneath
// its canonical root and links to documents elsewhere (docs/ops-documents.md).
// Every record is filed under the canonical key, which is also the root
// every file path is relative to.
type OpsDocuments struct {
	Store *store.Store
	// Home is the person's home directory, which the instruction-file
	// inventory reads beside the Project.
	Home string
	Now  func() time.Time
}

func (o *OpsDocuments) now() time.Time {
	if o.Now != nil {
		return o.Now()
	}
	return time.Now()
}

func opsError(status int, code, message string) error {
	return &WorkError{Status: status, Code: code, Message: message}
}

func mapOpsError(err error) error {
	var typed *WorkError
	switch {
	case err == nil:
		return nil
	case errors.As(err, &typed):
		return typed
	case errors.Is(err, store.ErrNoOps):
		return opsError(http.StatusNotFound, "ops_not_found", "No Ops document or collection has that id in this Project.")
	case errors.Is(err, store.ErrOpsFull):
		return opsError(http.StatusInsufficientStorage, "ops_full", "This Project's Ops list holds as many documents or collections as it keeps.")
	case errors.Is(err, store.ErrDirectTodoFull):
		return opsError(http.StatusInsufficientStorage, "direct_todos_full", "This Session holds as many direct to-dos as it keeps.")
	case errors.Is(err, projectfiles.ErrNameTaken):
		return opsError(http.StatusConflict, "ops_name_taken", "A file with that name already exists. Choose another name.")
	case errors.Is(err, projectfiles.ErrRenameCaseOnly):
		return opsError(http.StatusConflict, "ops_rename_case_only", "A rename must change more than letter case.")
	case errors.Is(err, projectfiles.ErrRenameInstruction):
		return opsError(http.StatusConflict, "ops_rename_instruction_file", "Instruction, Skill and command files keep their names.")
	case errors.Is(err, projectfiles.ErrRenameUnsupported):
		return opsError(http.StatusConflict, "ops_rename_unsupported", "This volume cannot rename the file safely; rename it in the Finder or a terminal.")
	case errors.Is(err, os.ErrPermission):
		return opsError(http.StatusForbidden, "file_permission", "This file cannot be read or saved with this machine's permissions.")
	case errors.Is(err, opsdocs.ErrNoRepository):
		return opsError(http.StatusConflict, "ops_not_git", "This Project is not in a git repository.")
	case errors.Is(err, projectfiles.ErrUnknown), errors.Is(err, projectfiles.ErrUnsafe),
		errors.Is(err, projectfiles.ErrLarge), errors.Is(err, projectfiles.ErrText),
		errors.Is(err, projectfiles.ErrChanged), errors.Is(err, projectfiles.ErrReadOnly),
		errors.Is(err, os.ErrNotExist):
		status, code, detail := projectfiles.Refusal(err)
		return opsError(status, code, detail)
	}
	return mapWorkV2Error(err)
}

// OpsView is one entry as the wire shows it, its file state checked now.
func (o *OpsDocuments) view(ctx context.Context, e domain.Entry) contract.OpsEntry {
	out := contract.OpsEntry{ID: e.ID, Kind: contract.OpsEntryKind(e.Kind), Title: e.Title,
		TitleSource: contract.OpsTitleSource(e.TitleSource), Collection: e.Collection, Position: e.Position,
		Path: e.Path, URL: e.URL, Type: contract.OpsEntryType(e.Type), Status: contract.OpsEntryStatus(e.Status),
		AccessNote: e.AccessNote, CreatedAt: e.CreatedAt, UpdatedAt: e.UpdatedAt}
	if e.Kind == domain.KindFile {
		state, _, err := projectfiles.StatTree(e.ProjectKey, e.Path)
		switch {
		case err == nil && state == projectfiles.TreeMissing:
			out.Status = contract.OpsEntryStatusMissing
		case err != nil || state != projectfiles.TreeRegular:
			out.Status, out.AccessNote = contract.OpsEntryStatusUnreadable, "The file is not a plain file this machine can read."
		default:
			out.Status = contract.OpsEntryStatusReady
			out.Type = contract.OpsEntryType(domain.FileType(e.Path, projectfiles.SniffText(e.ProjectKey, e.Path)))
		}
	}
	if e.ResolveTodo != "" {
		state, err := o.Store.OpsTodoState(ctx, e.ResolveTodo)
		if err != nil {
			state = "missing"
		}
		out.Resolve = &contract.OpsResolve{Terminal: e.ResolveTerminal, Todo: e.ResolveTodo, TodoState: contract.OpsTodoState(state)}
	}
	return out
}

func collectionWire(c domain.Collection) contract.OpsCollection {
	return contract.OpsCollection{ID: c.ID, Name: c.Name, Position: c.Position}
}

// List is every collection and entry of the Project filed under key.
func (o *OpsDocuments) List(ctx context.Context, key string) (contract.OpsList, error) {
	o.settle(ctx, key)
	collections, entries, err := o.Store.OpsList(ctx, key)
	if err != nil {
		return contract.OpsList{}, mapOpsError(err)
	}
	out := contract.OpsList{ProjectKey: key, Collections: []contract.OpsCollection{}, Entries: []contract.OpsEntry{}}
	for _, c := range collections {
		out.Collections = append(out.Collections, collectionWire(c))
	}
	for _, e := range entries {
		out.Entries = append(out.Entries, o.view(ctx, e))
	}
	return out, nil
}

// Entry is one stored entry. key, when not empty, must be its Project.
func (o *OpsDocuments) Entry(ctx context.Context, key, id string) (domain.Entry, error) {
	if !domain.ValidEntryID(id) {
		return domain.Entry{}, opsError(http.StatusNotFound, "ops_not_found", "No Ops document or collection has that id in this Project.")
	}
	e, err := o.Store.OpsEntry(ctx, id)
	if err == nil && key != "" && e.ProjectKey != key {
		err = store.ErrNoOps
	}
	return e, mapOpsError(err)
}

// Read is one entry, with the text of a readable file.
func (o *OpsDocuments) Read(ctx context.Context, key, id string) (contract.OpsRead, error) {
	e, err := o.Entry(ctx, key, id)
	if err != nil {
		return contract.OpsRead{}, err
	}
	o.settle(ctx, e.ProjectKey)
	if e, err = o.Entry(ctx, key, id); err != nil {
		return contract.OpsRead{}, err
	}
	out := contract.OpsRead{Entry: o.view(ctx, e)}
	if e.Kind == domain.KindFile && out.Entry.Status == contract.OpsEntryStatusReady &&
		out.Entry.Type != contract.OpsEntryTypeFile {
		content, err := projectfiles.ReadTree(e.ProjectKey, e.Path)
		if err == nil {
			out.Content, out.Version = content.Text, content.Version
		} else {
			out.Entry.Status = contract.OpsEntryStatusUnreadable
			_, _, out.Entry.AccessNote = projectfiles.Refusal(err)
		}
	}
	return out, nil
}

func (o *OpsDocuments) checkCollection(tx *store.WorkV2Tx, key, collection string) error {
	if collection == "" {
		return nil
	}
	c, err := tx.OpsCollection(collection)
	if err != nil || c.ProjectKey != key {
		return opsError(http.StatusNotFound, "ops_collection_not_found", "No collection has that id in this Project.")
	}
	return nil
}

func cleanTitle(title string) (string, error) {
	t, ok := domain.CleanText(title, domain.TitleBytesLimit)
	if !ok {
		return "", opsError(http.StatusBadRequest, "ops_bad_title", "A title is one line of at most 200 bytes.")
	}
	return t, nil
}

// NewOpsEntry is a create request for a link or an existing file.
type NewOpsEntry struct {
	Kind, URL, Path, Title, Collection string
}

// Create adds a link or an existing file.
func (o *OpsDocuments) Create(ctx context.Context, key string, n NewOpsEntry) (contract.OpsEntry, error) {
	now := o.now().Unix()
	e := domain.Entry{ID: domain.NewEntryID(), ProjectKey: key, Kind: n.Kind, Collection: n.Collection,
		Status: domain.StatusReady, CreatedAt: now, UpdatedAt: now}
	switch n.Kind {
	case domain.KindLink:
		u, typ, err := domain.ClassifyURL(n.URL)
		if err != nil {
			return contract.OpsEntry{}, opsError(http.StatusBadRequest, "ops_bad_url", "An Ops link is an http or https URL of at most 2048 bytes.")
		}
		e.URL, e.Type, e.Title, e.TitleSource = u, typ, domain.Clip(u, domain.TitleBytesLimit), domain.TitleFile
	case domain.KindFile:
		if _, err := projectfiles.TreePath(n.Path, true); err != nil {
			return contract.OpsEntry{}, opsError(http.StatusBadRequest, "bad_tree_path", "Choose a file within this Project.")
		}
		state, _, err := projectfiles.StatTree(key, n.Path)
		if err != nil || state != projectfiles.TreeRegular {
			return contract.OpsEntry{}, opsError(http.StatusNotFound, "file_not_found", "That file is not a plain file in this Project.")
		}
		e.Path, e.Type = n.Path, domain.FileType(n.Path, projectfiles.SniffText(key, n.Path))
		e.Title, e.TitleSource = domain.FileTitle(n.Path), domain.TitleFile
	default:
		return contract.OpsEntry{}, opsError(http.StatusBadRequest, "bad_request", "Add a link, a file, a Markdown file or a collection.")
	}
	if n.Title != "" {
		t, err := cleanTitle(n.Title)
		if err != nil {
			return contract.OpsEntry{}, err
		}
		e.Title, e.TitleSource = t, domain.TitleUser
	}
	err := o.Store.WriteWorkV2(ctx, func(tx *store.WorkV2Tx) error {
		if err := o.checkCollection(tx, key, e.Collection); err != nil {
			return err
		}
		pos, err := tx.OpsNextPosition(key, e.Collection)
		if err != nil {
			return err
		}
		e.Position = pos
		if err := tx.InsertOpsEntry(e); err != nil {
			return err
		}
		return nil
	})
	if err != nil {
		return contract.OpsEntry{}, mapOpsError(err)
	}
	return o.view(ctx, e), nil
}

// NewOpsMarkdown creates clawdline-ops/<name>.md.
type NewOpsMarkdown struct {
	Name, Content, Collection string
	// Git is "include", "exclude" or "" (no choice given).
	Git string
}

// CreateMarkdown writes a new Markdown file and records it. When the folder
// does not exist yet and the root is in git, the person must choose whether
// git sees it.
func (o *OpsDocuments) CreateMarkdown(ctx context.Context, key string, n NewOpsMarkdown) (contract.OpsEntry, error) {
	name, err := domain.MarkdownFileName(n.Name)
	if err != nil {
		return contract.OpsEntry{}, opsError(http.StatusBadRequest, "ops_bad_name", "A Markdown name has no slash or leading dot and is at most 252 bytes.")
	}
	if n.Git != "" && n.Git != "include" && n.Git != "exclude" {
		return contract.OpsEntry{}, opsError(http.StatusBadRequest, "bad_request", "git is include or exclude.")
	}
	setup := opsdocs.Setup(ctx, key)
	if !setup.FolderExists && setup.Git == contract.OpsGitStateRepo {
		if n.Git == "" {
			return contract.OpsEntry{}, opsError(http.StatusConflict, "ops_git_choice_required",
				"Choose whether git sees the clawdline-ops folder before its first file is created.")
		}
		if err := opsdocs.SetExcluded(ctx, key, n.Git == "exclude"); err != nil {
			return contract.OpsEntry{}, mapOpsError(err)
		}
	}
	// Refuse a full list or a wrong collection before the file is written.
	if err := o.precheck(ctx, key, n.Collection); err != nil {
		return contract.OpsEntry{}, err
	}
	if _, err := projectfiles.CreateTreeText(key, domain.Folder, name, n.Content); err != nil {
		return contract.OpsEntry{}, mapOpsError(err)
	}
	return o.Create(ctx, key, NewOpsEntry{Kind: domain.KindFile, Path: domain.Folder + "/" + name, Collection: n.Collection})
}

func (o *OpsDocuments) precheck(ctx context.Context, key, collection string) error {
	collections, entries, err := o.Store.OpsList(ctx, key)
	if err != nil {
		return mapOpsError(err)
	}
	if len(entries) >= domain.EntriesPerProjectLimit {
		return mapOpsError(store.ErrOpsFull)
	}
	if collection == "" {
		return nil
	}
	for _, c := range collections {
		if c.ID == collection {
			return nil
		}
	}
	return opsError(http.StatusNotFound, "ops_collection_not_found", "No collection has that id in this Project.")
}

// CreateCollection adds a named collection at the end.
func (o *OpsDocuments) CreateCollection(ctx context.Context, key, name string) (contract.OpsCollection, error) {
	clean, ok := domain.CleanText(name, domain.CollectionNameBytesLimit)
	if !ok {
		return contract.OpsCollection{}, opsError(http.StatusBadRequest, "ops_bad_name", "A collection name is one line of at most 100 bytes.")
	}
	c := domain.Collection{ID: domain.NewCollectionID(), ProjectKey: key, Name: clean}
	err := o.Store.WriteWorkV2(ctx, func(tx *store.WorkV2Tx) error {
		var err error
		if c, err = tx.InsertOpsCollection(c, o.now().Unix()); err != nil {
			return err
		}
		return nil
	})
	return collectionWire(c), mapOpsError(err)
}

// OpsPatch is a PATCH body. Nil fields are not changed.
type OpsPatch struct {
	Title           *string
	Collection      *string
	Position        *int64
	Name            *string
	Rename          *string
	Content         *string
	ExpectedVersion *string
}

// UpdateCollection renames or moves a collection.
func (o *OpsDocuments) UpdateCollection(ctx context.Context, key, id string, p OpsPatch) (contract.OpsCollection, error) {
	if p.Title != nil || p.Collection != nil || p.Rename != nil || p.Content != nil || p.ExpectedVersion != nil ||
		p.Name == nil && p.Position == nil {
		return contract.OpsCollection{}, opsError(http.StatusBadRequest, "bad_request", "A collection takes a name and a position.")
	}
	var out domain.Collection
	err := o.Store.WriteWorkV2(ctx, func(tx *store.WorkV2Tx) error {
		c, err := tx.OpsCollection(id)
		if err != nil || c.ProjectKey != key {
			return store.ErrNoOps
		}
		if p.Name != nil {
			clean, ok := domain.CleanText(*p.Name, domain.CollectionNameBytesLimit)
			if !ok {
				return opsError(http.StatusBadRequest, "ops_bad_name", "A collection name is one line of at most 100 bytes.")
			}
			c.Name = clean
		}
		if p.Position != nil {
			c.Position = *p.Position
		}
		if err := tx.PutOpsCollection(c, o.now().Unix()); err != nil {
			return err
		}
		out = c
		return nil
	})
	return collectionWire(out), mapOpsError(err)
}

// Update changes an entry's title, collection or position, renames its
// file, or saves its text. version is the saved text's new version.
func (o *OpsDocuments) Update(ctx context.Context, key, id string, p OpsPatch) (contract.OpsEntry, string, error) {
	if p.Name != nil {
		return contract.OpsEntry{}, "", opsError(http.StatusBadRequest, "bad_request", "An entry's shown name is its title.")
	}
	e, err := o.Entry(ctx, key, id)
	if err != nil {
		return contract.OpsEntry{}, "", err
	}
	if (p.Content == nil) != (p.ExpectedVersion == nil) {
		return contract.OpsEntry{}, "", opsError(http.StatusBadRequest, "bad_request", "A save names the content and the version it replaces.")
	}
	if (p.Rename != nil || p.Content != nil) && e.Kind != domain.KindFile {
		return contract.OpsEntry{}, "", opsError(http.StatusBadRequest, "bad_request", "Only a file entry is renamed or saved.")
	}
	if p.Rename != nil && p.Content != nil {
		return contract.OpsEntry{}, "", opsError(http.StatusBadRequest, "bad_request", "Rename and save separately.")
	}
	version := ""
	if p.Content != nil {
		saved, err := projectfiles.SaveTree(key, e.Path, *p.ExpectedVersion, *p.Content)
		if err != nil {
			return contract.OpsEntry{}, "", mapOpsError(err)
		}
		version = saved.Version
	}
	if p.Rename != nil {
		if err := o.rename(ctx, e, *p.Rename); err != nil {
			return contract.OpsEntry{}, "", err
		}
		if e, err = o.Entry(ctx, key, id); err != nil {
			return contract.OpsEntry{}, "", err
		}
	}
	var out domain.Entry
	err = o.Store.WriteWorkV2(ctx, func(tx *store.WorkV2Tx) error {
		cur, err := tx.OpsEntry(id)
		if err != nil {
			return err
		}
		if p.Title != nil {
			t, err := cleanTitle(*p.Title)
			if err != nil {
				return err
			}
			cur.Title, cur.TitleSource = t, domain.TitleUser
		}
		if p.Collection != nil && *p.Collection != cur.Collection {
			if err := o.checkCollection(tx, key, *p.Collection); err != nil {
				return err
			}
			cur.Collection = *p.Collection
			if p.Position == nil {
				if cur.Position, err = tx.OpsNextPosition(key, cur.Collection); err != nil {
					return err
				}
			}
		}
		if p.Position != nil {
			cur.Position = *p.Position
		}
		cur.UpdatedAt = o.now().Unix()
		if err := tx.PutOpsEntry(cur); err != nil {
			return err
		}
		out = cur
		return nil
	})
	if err != nil {
		return contract.OpsEntry{}, "", mapOpsError(err)
	}
	return o.view(ctx, out), version, nil
}

// rename records the rename as pending, renames the file by a second link,
// moves the record, and settles the pending row.
func (o *OpsDocuments) rename(ctx context.Context, e domain.Entry, newName string) error {
	if err := domain.RenameTarget(newName); err != nil {
		return opsError(http.StatusBadRequest, "ops_bad_name", "A file name has no slash or leading dot.")
	}
	if projectfiles.Instruction(e.ProjectKey, o.Home, e.Path) {
		return mapOpsError(projectfiles.ErrRenameInstruction)
	}
	dir := ""
	if i := strings.LastIndex(e.Path, "/"); i >= 0 {
		dir = e.Path[:i+1]
	}
	pending := domain.PendingRename{EntryID: e.ID, ProjectKey: e.ProjectKey, From: e.Path, To: dir + newName}
	if err := o.Store.WriteWorkV2(ctx, func(tx *store.WorkV2Tx) error { return tx.PutOpsPendingRename(pending) }); err != nil {
		return mapOpsError(err)
	}
	_, err := projectfiles.RenameTree(e.ProjectKey, e.Path, newName, func(to string) error {
		return o.Store.WriteWorkV2(ctx, func(tx *store.WorkV2Tx) error {
			cur, err := tx.OpsEntry(e.ID)
			if err != nil {
				return err
			}
			cur.Path, cur.UpdatedAt = to, o.now().Unix()
			if cur.TitleSource == domain.TitleFile {
				cur.Title = domain.FileTitle(to)
			}
			if err := tx.PutOpsEntry(cur); err != nil {
				return err
			}
			return tx.DeleteOpsPendingRename(e.ID)
		})
	})
	if err != nil {
		// The disk is as it was or the record moved; a pending row left
		// behind is settled on the next read or start.
		_ = o.Store.WriteWorkV2(context.WithoutCancel(ctx), func(tx *store.WorkV2Tx) error {
			cur, readErr := tx.OpsEntry(e.ID)
			if readErr == nil && cur.Path == e.Path {
				return tx.DeleteOpsPendingRename(e.ID)
			}
			return nil
		})
		return mapOpsError(err)
	}
	return nil
}

// SettleRenames finishes or undoes every rename the daemon stopped in.
func (o *OpsDocuments) SettleRenames(ctx context.Context) error {
	pending, err := o.Store.OpsPendingRenames(ctx)
	if err != nil {
		return err
	}
	for _, p := range pending {
		o.settleOne(ctx, p)
	}
	return nil
}

func (o *OpsDocuments) settle(ctx context.Context, key string) {
	pending, err := o.Store.OpsPendingRenames(ctx)
	if err != nil {
		return
	}
	for _, p := range pending {
		if p.ProjectKey == key {
			o.settleOne(ctx, p)
		}
	}
}

func (o *OpsDocuments) settleOne(ctx context.Context, p domain.PendingRename) {
	e, err := o.Store.OpsEntry(ctx, p.EntryID)
	if err != nil {
		_ = o.Store.WriteWorkV2(ctx, func(tx *store.WorkV2Tx) error { return tx.DeleteOpsPendingRename(p.EntryID) })
		return
	}
	path, err := projectfiles.SettleRename(p.ProjectKey, p.From, p.To, e.Path)
	if err != nil {
		return
	}
	_ = o.Store.WriteWorkV2(ctx, func(tx *store.WorkV2Tx) error {
		cur, err := tx.OpsEntry(p.EntryID)
		if err != nil {
			return tx.DeleteOpsPendingRename(p.EntryID)
		}
		if cur.Path != path {
			cur.Path, cur.UpdatedAt = path, o.now().Unix()
			if cur.TitleSource == domain.TitleFile {
				cur.Title = domain.FileTitle(path)
			}
			if err := tx.PutOpsEntry(cur); err != nil {
				return err
			}
		}
		return tx.DeleteOpsPendingRename(p.EntryID)
	})
}

// Delete removes an entry's record, never its file, or removes a
// collection and moves its entries to none.
func (o *OpsDocuments) Delete(ctx context.Context, key, id string) error {
	err := o.Store.WriteWorkV2(ctx, func(tx *store.WorkV2Tx) error {
		switch {
		case domain.ValidEntryID(id):
			e, err := tx.OpsEntry(id)
			if err != nil || e.ProjectKey != key {
				return store.ErrNoOps
			}
			if err := tx.DeleteOpsEntry(id); err != nil {
				return err
			}
		case domain.ValidCollectionID(id):
			c, err := tx.OpsCollection(id)
			if err != nil || c.ProjectKey != key {
				return store.ErrNoOps
			}
			if err := tx.DeleteOpsCollection(c, o.now().Unix()); err != nil {
				return err
			}
		default:
			return store.ErrNoOps
		}
		return nil
	})
	return mapOpsError(err)
}

// ResolveRequest creates, without sending, the direct to-do that asks the
// Session conversation (behind terminal) to read a link, and marks the entry
// resolving, in one transaction. An earlier resolve to-do of this entry that
// was not yet read is completed.
func (o *OpsDocuments) ResolveRequest(ctx context.Context, key, id, terminal, conversation, actor string) (contract.OpsEntry, string, error) {
	e, err := o.Entry(ctx, key, id)
	if err != nil {
		return contract.OpsEntry{}, "", err
	}
	if e.Kind != domain.KindLink {
		return contract.OpsEntry{}, "", opsError(http.StatusBadRequest, "ops_not_link", "Only a link is read by a Session.")
	}
	now := o.now()
	td := work.DirectTodoV2{ID: newWorkID(), SessionID: conversation, CreatedBy: actor, CreatedAt: now, Version: 1}
	td.Text = domain.ResolveTodoText(e.ID, e.URL, td.ID)
	if len(td.Text) > directTodoTextLimit {
		return contract.OpsEntry{}, "", opsError(http.StatusRequestEntityTooLarge, "todo_too_large", "A direct to-do is at most 8 KiB.")
	}
	var out domain.Entry
	err = o.Store.WriteWorkV2(ctx, func(tx *store.WorkV2Tx) error {
		cur, err := tx.OpsEntry(id)
		if err != nil {
			return err
		}
		if cur.ResolveTodo != "" {
			prev, err := tx.DirectTodo(cur.ResolveTodo)
			if err == nil && prev.CompletedAt.IsZero() && prev.ReadAt.IsZero() {
				next := prev
				next.CompletedAt, next.CompletedBy = now, actor
				if err := tx.PutDirectTodo(prev, next); err != nil {
					return err
				}
			}
		}
		if err := tx.CreateDirectTodo(td); err != nil {
			return err
		}
		cur.Status, cur.AccessNote = domain.StatusResolving, ""
		cur.ResolveTerminal, cur.ResolveTodo, cur.UpdatedAt = terminal, td.ID, now.Unix()
		if err := tx.PutOpsEntry(cur); err != nil {
			return err
		}
		out = cur
		return nil
	})
	if err != nil {
		return contract.OpsEntry{}, "", mapOpsError(err)
	}
	return o.view(ctx, out), td.ID, nil
}

// OpsResolution is what the reading Session reports.
type OpsResolution struct {
	Todo, Title, Reason string
	NeedsAccess         bool
}

// Resolve records what the Session found. Only the entry's current resolve
// to-do may answer; a person's title is never replaced.
func (o *OpsDocuments) Resolve(ctx context.Context, id string, r OpsResolution) (contract.OpsEntry, error) {
	if _, err := o.Entry(ctx, "", id); err != nil {
		return contract.OpsEntry{}, err
	}
	var title, reason string
	if r.NeedsAccess {
		clean, ok := domain.CleanText(strings.ReplaceAll(r.Reason, "\n", " "), domain.NoteBytesLimit)
		if !ok || r.Title != "" {
			return contract.OpsEntry{}, opsError(http.StatusBadRequest, "ops_bad_reason", "Say what the person must do in at most 1000 bytes, without a title.")
		}
		reason = clean
	} else {
		t, err := cleanTitle(r.Title)
		if err != nil || r.Reason != "" {
			return contract.OpsEntry{}, opsError(http.StatusBadRequest, "ops_bad_title", "A title is one line of at most 200 bytes.")
		}
		title = t
	}
	var out domain.Entry
	err := o.Store.WriteWorkV2(ctx, func(tx *store.WorkV2Tx) error {
		cur, err := tx.OpsEntry(id)
		if err != nil {
			return err
		}
		if r.Todo == "" || cur.ResolveTodo != r.Todo {
			return opsError(http.StatusConflict, "resolve_stale", "This to-do no longer asks for this link; stop.")
		}
		if r.NeedsAccess {
			cur.Status, cur.AccessNote = domain.StatusNeedsAccess, reason
		} else {
			cur.Status, cur.AccessNote = domain.StatusReady, ""
			if cur.TitleSource != domain.TitleUser {
				cur.Title, cur.TitleSource = title, domain.TitleAgent
			}
		}
		cur.UpdatedAt = o.now().Unix()
		if err := tx.PutOpsEntry(cur); err != nil {
			return err
		}
		out = cur
		return nil
	})
	if err != nil {
		return contract.OpsEntry{}, mapOpsError(err)
	}
	return o.view(ctx, out), nil
}
