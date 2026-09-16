package app

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/sainteye/clawdline-go/internal/adapters/git"
	"github.com/sainteye/clawdline-go/internal/adapters/store"
	"github.com/sainteye/clawdline-go/internal/domain/task"
)

// Lander settles the obligation a delivery leaves behind.
type Lander struct {
	Store *store.Store
	Git   *git.Git
}

// Land records that a delivery reached its branch, after proving it did.
//
// The proof is ancestry, asked of git, in the repository the work was done in.
// Delivered is not reviewed and reviewed is not landed: this is the last rung,
// and it is the one rung a caller must not be able to assert on its own word.
func (l Lander) Land(ctx context.Context, taskID, repo, commit, branch string) error {
	ok, err := l.Git.IsAncestor(ctx, repo, commit, branch)
	if err != nil {
		return task.Refusal{Code: "unverified_landing", Detail: err.Error()}
	}
	if !ok {
		return task.Refusal{
			Code:   "unverified_landing",
			Detail: fmt.Sprintf("%s is not an ancestor of %s in %s", commit, branch, repo),
		}
	}
	payload, _ := json.Marshal(map[string]string{
		"repo": repo, "commit": commit, "branch": branch,
	})
	_, err = l.Store.Commit(ctx, "land:"+taskID+":"+commit,
		[]store.Event{{Kind: "task.landed", Subject: taskID, Payload: payload}},
		[]store.Change{{Close: "landing:" + taskID}})
	return err
}

// OpenLanding records that a delivery is waiting to reach its branch.
func (l Lander) OpenLanding(ctx context.Context, taskID, target string, mover task.Mover, at task.Obligation) error {
	at.ID = "landing:" + taskID
	at.Kind = task.KindLanding
	at.Subject = taskID
	at.Mover = mover
	at.Note = "not yet on " + target
	_, err := l.Store.Commit(ctx, "landing-open:"+taskID,
		[]store.Event{{Kind: "landing.opened", Subject: taskID}},
		[]store.Change{{Open: &at}})
	return err
}
