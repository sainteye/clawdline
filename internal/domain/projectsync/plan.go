package projectsync

import "sort"

// Why a carried file was left alone.
const (
	// KeepLocalEdit is a file somebody changed on this machine since the
	// mirror last wrote it (or that existed before the mirror, differently).
	KeepLocalEdit = "local_edit"
	// KeepTracked is a file git tracks here: the repository owns it.
	KeepTracked = "tracked"
	// KeepUnsafe is a path that passes through a link or is not a file.
	KeepUnsafe = "unsafe_path"
)

// Here is what this machine has at one carried path: its hash ("" when there
// is no file), whether git tracks it, and whether it can be touched at all.
type Here struct {
	SHA256  string
	Tracked bool
	Unsafe  bool
}

// Kept is a file an apply did not touch, and why.
type Kept struct {
	Path   string `json:"path"`
	Reason string `json:"reason"`
}

// Steps is what one apply does to the files of one checkout.
type Steps struct {
	Write  []string          // paths to write from the entry
	Delete []string          // paths the source dropped and nobody edited here
	Kept   []Kept            // paths deliberately left alone
	Owned  map[string]string // what the mirror owns afterwards: path → hash
}

// Plan decides one apply. The source's content wins only over what the mirror
// itself wrote, or over nothing; a file that differs from both what the
// mirror last wrote and what the source now sends was changed here, and is
// kept and named rather than overwritten. A mirror is read-only by intent, and
// that intent does not extend to destroying somebody's edit.
func Plan(e Entry, owned map[string]string, here map[string]Here) Steps {
	s := Steps{Owned: map[string]string{}}
	offered := map[string]bool{}
	for _, f := range e.Files {
		offered[f.Path] = true
		h := here[f.Path]
		switch {
		case h.Unsafe:
			s.Kept = append(s.Kept, Kept{f.Path, KeepUnsafe})
		case h.Tracked:
			s.Kept = append(s.Kept, Kept{f.Path, KeepTracked})
		case h.SHA256 == f.SHA256:
			s.Owned[f.Path] = f.SHA256
		case h.SHA256 == "" || h.SHA256 == owned[f.Path]:
			s.Write = append(s.Write, f.Path)
			s.Owned[f.Path] = f.SHA256
		default:
			s.Kept = append(s.Kept, Kept{f.Path, KeepLocalEdit})
		}
	}
	for p, sum := range owned {
		if offered[p] {
			continue
		}
		h := here[p]
		switch {
		case h.Unsafe || h.Tracked || h.SHA256 == "":
			// Gone already, or no longer the mirror's to remove.
		case h.SHA256 == sum:
			s.Delete = append(s.Delete, p)
		default:
			s.Kept = append(s.Kept, Kept{p, KeepLocalEdit})
		}
	}
	sort.Strings(s.Write)
	sort.Strings(s.Delete)
	sort.Slice(s.Kept, func(i, j int) bool { return s.Kept[i].Path < s.Kept[j].Path })
	return s
}
