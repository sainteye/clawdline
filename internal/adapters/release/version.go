package release

import (
	"strconv"
	"strings"
)

// Version is a release tag, vMAJOR.MINOR.PATCH with an optional
// -beta.N style pre-release.
type Version struct {
	Major, Minor, Patch int
	Pre                 string
}

// ParseVersion reads a tag such as v0.10.0 or v0.11.0-beta.2.
func ParseVersion(s string) (Version, error) {
	bad := func() (Version, error) {
		return Version{}, refuse(CodeVersionUnparseable, "%q is not a release version like v0.10.0", s)
	}
	rest, ok := strings.CutPrefix(s, "v")
	if !ok {
		return bad()
	}
	core, pre, _ := strings.Cut(rest, "-")
	parts := strings.Split(core, ".")
	if len(parts) != 3 {
		return bad()
	}
	var n [3]int
	for i, p := range parts {
		v, err := strconv.Atoi(p)
		if err != nil || v < 0 || (len(p) > 1 && p[0] == '0') {
			return bad()
		}
		n[i] = v
	}
	if strings.Contains(s, "-") && pre == "" {
		return bad()
	}
	return Version{Major: n[0], Minor: n[1], Patch: n[2], Pre: pre}, nil
}

// Compare is -1, 0 or 1 as a is older than, the same as, or newer than b. A
// pre-release is older than its release; pre-releases compare by their dotted
// fields, numerically where both are numbers.
func Compare(a, b Version) int {
	for _, d := range [][2]int{{a.Major, b.Major}, {a.Minor, b.Minor}, {a.Patch, b.Patch}} {
		if d[0] != d[1] {
			if d[0] < d[1] {
				return -1
			}
			return 1
		}
	}
	switch {
	case a.Pre == b.Pre:
		return 0
	case a.Pre == "":
		return 1
	case b.Pre == "":
		return -1
	}
	af, bf := strings.Split(a.Pre, "."), strings.Split(b.Pre, ".")
	for i := 0; i < len(af) && i < len(bf); i++ {
		if af[i] == bf[i] {
			continue
		}
		ai, aerr := strconv.Atoi(af[i])
		bi, berr := strconv.Atoi(bf[i])
		if aerr == nil && berr == nil {
			if ai < bi {
				return -1
			}
			return 1
		}
		if af[i] < bf[i] {
			return -1
		}
		return 1
	}
	switch {
	case len(af) < len(bf):
		return -1
	case len(af) > len(bf):
		return 1
	}
	return 0
}

func (v Version) String() string {
	s := "v" + strconv.Itoa(v.Major) + "." + strconv.Itoa(v.Minor) + "." + strconv.Itoa(v.Patch)
	if v.Pre != "" {
		s += "-" + v.Pre
	}
	return s
}
