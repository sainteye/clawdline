package orchestrator

import "context"

type branchPresenceReader func(context.Context, string, string) (bool, bool)

// branchPresence is a lazy reading for this projection only. Requested names
// and results are bounded by the records already selected for this repository;
// no branch evidence survives into the next request or a destructive sweep.
func (b *Broker) branchPresence(records []Record) branchPresenceReader {
	requests := map[string][]string{}
	for _, r := range records {
		if r.State.Terminal() && r.Worktree != nil {
			requests[r.Worktree.Repository] = append(requests[r.Worktree.Repository], r.Worktree.Branch)
		}
	}
	readings := map[string]map[string]bool{}
	return func(ctx context.Context, repo, branch string) (bool, bool) {
		presence, read := readings[repo]
		if !read {
			// Even failure is remembered for this request, so it does not
			// retry the entire batch for every row. Individual reads retain
			// the old unknown/error behavior, including on older Git builds
			// or an argument list too large for the operating system.
			presence, _ = b.Git.ReadBranchPresence(ctx, repo, requests[repo])
			readings[repo] = presence
		}
		if exists, known := presence[branch]; known {
			return exists, true
		}
		return b.Git.BranchExists(ctx, repo, branch)
	}
}
