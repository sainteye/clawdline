package http

import (
	"context"
	"net/http"
	"os"
	"time"

	"github.com/sainteye/clawdline/internal/adapters/devstack"
	"github.com/sainteye/clawdline/internal/adapters/projects"
	"github.com/sainteye/clawdline/internal/contract"
)

// devStacksRoute answers the input bar's server list (devstacks.schema.json).
//
// The projects looked at are the ones `Controller.refreshStacks` looks at:
// every directory the icon registry names and every live session's working
// directory, each walked up for a `.devstack.json`. Each declared port is then
// asked whether anything is listening, which is the Swift app's Tier 0.
//
// **Nothing the file names is run.** Not `status`, which the Swift app ran on
// a timer for a stack a person had trusted, and not `up`, `down`, `restart`
// or `logs`, which it ran from a button in its native window. This route is a
// GET a paired phone may make; a stack whose state only its `status` command
// knows is answered `unknown` with `status_not_run`, and the bar says in words
// that it is not run here and where to run it instead.
//
// **Read level**, and no cache: a discovery is a few hundred stats, and every
// port on loopback answers at once or not at all. The bar asks while its list
// is open, which is when the Swift app refreshed it.
func (s *Server) devStacksRoute(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeRefusal(w, http.StatusMethodNotAllowed, "method_not_allowed",
			"The server list is read with GET. This daemon runs none of a project's own commands.")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()
	home, _ := os.UserHomeDir()
	found := devstack.Discover(projects.RegistryPaths(), s.liveDirectories(ctx), home)
	stacks := devstack.Read(ctx, found.Specs, devstack.Listening)
	reply := contract.DevStacksReply{
		Stacks:     make([]contract.DevStack, 0, len(stacks)),
		ObservedAt: float64(time.Now().UnixMilli()) / 1000,
		Truncated:  found.Truncated,
	}
	for _, st := range stacks {
		reply.Stacks = append(reply.Stacks, s.wireDevStack(st))
	}
	writeJSON(w, reply)
}

func (s *Server) wireDevStack(st devstack.Stack) contract.DevStack {
	out := contract.DevStack{
		Name:      st.Name,
		Root:      st.Root,
		State:     contract.DevStackState(st.State),
		Unknown:   contract.DevStackUnknown(st.Unknown),
		Processes: make([]contract.DevProcess, 0, len(st.Processes)),
		Commands:  make([]contract.DevStackCommand, 0, len(st.Commands)),
	}
	if s.icons != nil {
		out.Icon = wireIcon(s.icons.For(st.Root))
	}
	for _, p := range st.Processes {
		out.Processes = append(out.Processes, contract.DevProcess{
			Name:  p.Name,
			State: contract.DevProcessState(p.State),
			Port:  int64(p.Port),
			URL:   p.URL,
		})
	}
	for _, c := range st.Commands {
		out.Commands = append(out.Commands, contract.DevStackCommand(c))
	}
	return out
}
