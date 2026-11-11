package cloudops

import "github.com/sainteye/clawdline/internal/domain/cloudvocab"

// machineOnlyWords are the vocabulary rows flagged `local_exemption`: words
// whose local route answers only this machine, so a Cloud viewer reaching
// them borrows this machine's own exemption. An invited browser never may
// (Authority.Invited). The set is read from the table rather than listed, so
// a new exempt word is covered the moment its row says so.
var machineOnlyWords = func() map[string]bool {
	ops, err := cloudvocab.Operations()
	if err != nil {
		// The table is embedded and validated by its own tests; a build that
		// cannot read it cannot serve any word, so refusing loudly is right.
		panic("cloudops: the vocabulary table does not decode: " + err.Error())
	}
	out := map[string]bool{}
	for _, o := range ops {
		if o.Local != nil && o.Local.LocalExemption {
			out[o.Op] = true
		}
	}
	return out
}()
