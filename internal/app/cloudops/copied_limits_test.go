package cloudops

import (
	"sort"
	"testing"

	"github.com/sainteye/clawdline/internal/adapters/projectfiles"
	"github.com/sainteye/clawdline/internal/adapters/store"
	"github.com/sainteye/clawdline/internal/domain/capacity"
	"github.com/sainteye/clawdline/internal/domain/cloudvocab"
	"github.com/sainteye/clawdline/internal/domain/squad"
	"github.com/sainteye/clawdline/internal/domain/squadpack"
)

// copiedConstants is every Go constant a limit in the vocabulary table is a
// copy of, by the name the table's `copies` gives it. The generated decoders
// read the table's number; this is what keeps that number the constant's.
var copiedConstants = map[string]int64{
	"capacity.AntigravityConsentRequestBytesLimit": capacity.AntigravityConsentRequestBytesLimit,
	"ViewerEventsBatchBytesLimit":                  ViewerEventsBatchBytesLimit,
	"SessionRowIDBytesLimit":                       SessionRowIDBytesLimit,
	"defaultModelsCloudBodyLimit":                  defaultModelsCloudBodyLimit,
	"intentTextLimit":                              intentTextLimit,
	"projectSyncCloudBodyLimit":                    projectSyncCloudBodyLimit,
	"scheduleMaximumBytes":                         scheduleMaximumBytes,
	"snippetMaximumBytes":                          snippetMaximumBytes,
	"verificationCloudBodyLimit":                   verificationCloudBodyLimit,
	"verificationCriteriaMaximum-1":                verificationCriteriaMaximum - 1,
	"voiceRate":                                    voiceRate,
	"workV2CloudBodyLimit":                         workV2CloudBodyLimit,
	"workV2CloudImageBodyLimit":                    workV2CloudImageBodyLimit,
	"projectfiles.MaxWriteBytes":                   projectfiles.MaxWriteBytes,
	"squad.MaxSquadRequestBytes":                   squad.MaxSquadRequestBytes,
	"squadpack.MaxRequestBytes":                    squadpack.MaxRequestBytes,
	"store.MaxSquadEventPageRows":                  store.MaxSquadEventPageRows,
}

// Every limit the table copies equals the constant it names, and every
// constant here is named by some row.
func TestCopiedLimitsAreTheirConstants(t *testing.T) {
	ops, err := cloudvocab.Operations()
	if err != nil {
		t.Fatal(err)
	}
	named := map[string]int{}
	for _, row := range ops {
		fields := make([]string, 0, len(row.Decode.Fields))
		for field := range row.Decode.Fields {
			fields = append(fields, field)
		}
		sort.Strings(fields)
		for _, field := range fields {
			for _, c := range cloudvocab.CopiedLimits(field, row.Decode.Fields[field]) {
				named[c.Constant]++
				want, known := copiedConstants[c.Constant]
				if !known {
					t.Errorf("%s %s %s copies %s, which this test does not know", row.Op, c.Path, c.Limit, c.Constant)
					continue
				}
				if c.Value != want {
					t.Errorf("%s %s %s is %d, and %s is %d", row.Op, c.Path, c.Limit, c.Value, c.Constant, want)
				}
			}
		}
	}
	for constant := range copiedConstants {
		if named[constant] == 0 {
			t.Errorf("%s: no row copies it", constant)
		}
	}
}
