package projectassistantstore

import (
	"testing"

	app "github.com/sainteye/clawdline/internal/app/projectassistant"
	pa "github.com/sainteye/clawdline/internal/domain/projectassistant"
)

func TestOnlyAPermissionNewlyPendingIsAnnounced(t *testing.T) {
	a := aggregate{Permissions: map[string]pa.PermissionOffer{
		"old":  {ID: "old", State: pa.PermissionPending},
		"done": {ID: "done", State: pa.PermissionResolved},
	}}
	m := app.Mutation{Permissions: []pa.PermissionOffer{
		{ID: "old", State: pa.PermissionPending},
		{ID: "done", State: pa.PermissionResolved},
		{ID: "new", State: pa.PermissionPending, Kind: pa.DecisionOfferKind},
		{ID: "new", State: pa.PermissionPending, Kind: pa.DecisionOfferKind},
		{ID: "gone", State: pa.PermissionCancelled},
	}}
	got := newlyPendingPermissions(a, m)
	if len(got) != 1 || got[0].ID != "new" {
		t.Fatalf("announced %+v", got)
	}
}
