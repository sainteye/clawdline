package capacity

import (
	"reflect"
	"testing"

	"github.com/sainteye/clawdline/internal/contract"
)

func TestWorkGateCapacityNamesAndPoliciesAreStable(t *testing.T) {
	type policy struct {
		limit    int64
		class    Class
		unit     Unit
		action   Action
		decider  Decider
		told     []Channel
		source   string
		projects bool
	}
	want := map[string]policy{
		WorkGateRoundDetailsPerItem:       {contract.WorkGateRoundDetailsPerItemLimit, Evidence, Rows, Refuse, Person, []Channel{Diagnostics, Notice, Health}, "internal/contract.WorkGateRoundDetailsPerItemLimit", true},
		WorkGateRoundDetailsPerStore:      {contract.WorkGateRoundDetailsPerStoreLimit, Evidence, Rows, Refuse, Person, []Channel{Diagnostics, Notice, Health}, "internal/contract.WorkGateRoundDetailsPerStoreLimit", true},
		WorkGateTasksPerRound:             {contract.WorkGateTasksPerRoundLimit, Buffer, Rows, Refuse, Daemon, []Channel{Diagnostics, Sender}, "internal/contract.WorkGateTasksPerRoundLimit", false},
		WorkGateClaimsPerRound:            {contract.WorkGateClaimsPerRoundLimit, Buffer, Rows, Refuse, Daemon, []Channel{Diagnostics, Sender}, "internal/contract.WorkGateClaimsPerRoundLimit", false},
		WorkGateEvidenceStringsPerClaim:   {contract.WorkGateEvidenceStringsPerClaimLimit, Buffer, Rows, Refuse, Daemon, []Channel{Diagnostics, Sender}, "internal/contract.WorkGateEvidenceStringsPerClaimLimit", false},
		WorkGateEvidenceStringBytes:       {contract.WorkGateEvidenceStringBytesLimit, Buffer, Bytes, Refuse, Daemon, []Channel{Diagnostics, Sender}, "internal/contract.WorkGateEvidenceStringBytesLimit", false},
		WorkGateResultBytes:               {contract.WorkGateResultBytesLimit, Buffer, Bytes, Refuse, Daemon, []Channel{Diagnostics, Sender}, "internal/contract.WorkGateResultBytesLimit", false},
		WorkGateEvidenceArtifactsPerTask:  {contract.WorkGateEvidenceArtifactsPerTaskLimit, Buffer, Rows, Refuse, Daemon, []Channel{Diagnostics, Sender}, "internal/contract.WorkGateEvidenceArtifactsPerTaskLimit", false},
		WorkGateEvidenceArtifactBytes:     {contract.WorkGateEvidenceArtifactBytesLimit, Buffer, Bytes, Refuse, Daemon, []Channel{Diagnostics, Sender}, "internal/contract.WorkGateEvidenceArtifactBytesLimit", false},
		WorkGateEvidenceTotalBytesPerTask: {contract.WorkGateEvidenceTotalBytesPerTaskLimit, Buffer, Bytes, Refuse, Daemon, []Channel{Diagnostics, Sender}, "internal/contract.WorkGateEvidenceTotalBytesPerTaskLimit", false},
		WorkGateRecentRoundsPerItemRead:   {contract.WorkGateRecentRoundsPerItemReadLimit, Observation, Rows, EvictOldest, Daemon, []Channel{Diagnostics}, "internal/contract.WorkGateRecentRoundsPerItemReadLimit", false},
		WorkGateDueRowsPerPass:            {contract.WorkGateDueRowsPerPassLimit, Observation, Rows, EvictOldest, Daemon, []Channel{Diagnostics}, "internal/contract.WorkGateDueRowsPerPassLimit", false},
		WorkGateRetryBackoffSeconds:       {contract.WorkGateRetryBackoffSecondsLimit, Cache, Seconds, Expire, Daemon, []Channel{Diagnostics}, "internal/contract.WorkGateRetryBackoffSecondsLimit", false},
		WorkGateOwnerOfflineGraceSeconds:  {contract.WorkGateOwnerOfflineGraceSecondsLimit, Observation, Seconds, EvictOldest, Daemon, []Channel{Diagnostics, Notice}, "internal/contract.WorkGateOwnerOfflineGraceSecondsLimit", false},
	}

	got := map[string]Entry{}
	for _, entry := range Register() {
		if _, ok := want[entry.Name]; ok {
			got[entry.Name] = entry
		}
	}
	if len(got) != len(want) {
		t.Fatalf("registered gate rows = %d, want %d", len(got), len(want))
	}
	for name, expected := range want {
		entry := got[name]
		if entry.Limit != expected.limit || entry.Class != expected.class || entry.Unit != expected.unit ||
			entry.AtLimit != expected.action || entry.EvictedBy != expected.decider || entry.Projects != expected.projects ||
			!reflect.DeepEqual(entry.Told, expected.told) || !reflect.DeepEqual(entry.Sources, []string{expected.source}) {
			t.Errorf("%s = %+v, want %+v", name, entry, expected)
		}
	}
}
