package http

import "github.com/sainteye/clawdline/internal/domain/capacity"

// capacityMeasureAreas is one capacity_measures_<area>.go per register area
// (capacity.Areas), each measuring the rows declared in the capacity
// package's register_<area>.go.
var capacityMeasureAreas = []struct {
	area     string
	measures func(*Server) map[string]func() capacity.Reading
}{
	{"antigravity", (*Server).antigravityCapacityMeasures},
	{"artifacts", (*Server).artifactsCapacityMeasures},
	{"board", (*Server).boardCapacityMeasures},
	{"broker", (*Server).brokerCapacityMeasures},
	{"cache", (*Server).cacheCapacityMeasures},
	{"callback", (*Server).callbackCapacityMeasures},
	{"cloud", (*Server).cloudCapacityMeasures},
	{"console", (*Server).consoleCapacityMeasures},
	{"daemon", (*Server).daemonCapacityMeasures},
	{"intent", (*Server).intentCapacityMeasures},
	{"iterm", (*Server).itermCapacityMeasures},
	{"memory", (*Server).memoryCapacityMeasures},
	{"places", (*Server).placesCapacityMeasures},
	{"projectfiles", (*Server).projectFilesCapacityMeasures},
	{"projectsync", (*Server).projectSyncCapacityMeasures},
	{"release", (*Server).releaseCapacityMeasures},
	{"session", (*Server).sessionCapacityMeasures},
	{"sessions", (*Server).sessionsCapacityMeasures},
	{"squad", (*Server).squadCapacityMeasures},
	{"squadpackage", (*Server).squadPackageCapacityMeasures},
	{"store", (*Server).storeCapacityMeasures},
	{"toollibrary", (*Server).toolLibraryCapacityMeasures},
	{"terminal", (*Server).terminalCapacityMeasures},
	{"update", (*Server).updateCapacityMeasures},
	{"work", (*Server).workCapacityMeasures},
	{"work_gate", (*Server).workGateCapacityMeasures},
}
