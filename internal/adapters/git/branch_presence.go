package git

import (
	"context"
	"fmt"
	"strings"
)

// ReadBranchPresence asks Git for the union of these branch patterns once.
// Its result contains only requested names. A missing key requires the caller
// to use BranchExists: legacy wildcard/trailing-slash patterns retain their
// individual Git interpretation. An error likewise requires individual reads,
// rather than turning a failed batch into known-absent branches.
func (g *Git) ReadBranchPresence(ctx context.Context, repo string, branches []string) (map[string]bool, error) {
	present := make(map[string]bool, len(branches))
	args := []string{"for-each-ref", "--format=%(refname)"}
	for _, branch := range branches {
		if strings.ContainsAny(branch, "*?[\\") || strings.HasSuffix(branch, "/") {
			continue
		}
		if _, seen := present[branch]; seen {
			continue
		}
		present[branch] = false
		args = append(args, "refs/heads/"+branch)
	}
	if len(present) == 0 {
		return present, nil
	}
	out, err := g.run(ctx, repo, args...)
	if err != nil {
		return nil, err
	}
	if out == "" {
		return present, nil
	}
	for _, ref := range strings.Split(out, "\n") {
		if !strings.HasPrefix(ref, "refs/heads/") || ref == "refs/heads/" {
			return nil, fmt.Errorf("git returned an invalid branch reference")
		}
		if _, asked := present[""]; asked {
			present[""] = true
		}
		// for-each-ref also matches a literal pattern up to a slash. A
		// namespace with children therefore exists in BranchExists even
		// when it is not itself a commit; Merged will still say unknown.
		name := strings.TrimPrefix(ref, "refs/heads/")
		for {
			if _, asked := present[name]; asked {
				present[name] = true
			}
			slash := strings.LastIndexByte(name, '/')
			if slash < 0 {
				break
			}
			name = name[:slash]
		}
	}
	return present, nil
}
