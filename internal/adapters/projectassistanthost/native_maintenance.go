package projectassistanthost

import (
	"context"

	app "github.com/sainteye/clawdline/internal/app/projectassistant"
	pa "github.com/sainteye/clawdline/internal/domain/projectassistant"
)

// qualifyNativeMaintenance composes the internal maintenance path for isolated
// qualification. No production binding supplies a provider or journal. In
// particular this never uses the regular provider's turn tools and MCP grants.
func (h *Host) qualifyNativeMaintenance(ctx context.Context, intent app.NativeMaintenanceIntent, journal app.NativeMaintenanceJournal, provider app.Provider) (app.NativeMaintenanceRecord, error) {
	if journal == nil || provider == nil {
		return app.NativeMaintenanceRecord{}, failure(pa.Disabled, "native_maintenance_qualification")
	}
	fence := intent.Binding.Fence
	check := func(ctx context.Context) error {
		h.mu.Lock()
		live := h.enabled && !h.closed && !h.cleanupPending
		h.mu.Unlock()
		if !live {
			return failure(pa.Disabled, "native_maintenance_host")
		}
		auth, err := h.human(ctx, fence.Key, true)
		if err != nil {
			return err
		}
		if err = auth.Feature.CheckWrite(); err != nil {
			return err
		}
		if auth.Source.MachineID != fence.Key.MachineID || auth.PrincipalID != intent.PrincipalID ||
			auth.ReaderID != intent.ReaderID || auth.AuthorizationRevision != intent.AuthorizationRevision {
			return failure(pa.ScopeDenied, "native_maintenance_authority")
		}
		return pa.CheckSource(auth.Source, true)
	}
	if err := check(ctx); err != nil {
		return app.NativeMaintenanceRecord{}, err
	}
	engine, err := h.runtime()
	if err != nil {
		return app.NativeMaintenanceRecord{}, err
	}
	return engine.QualifyNativeMaintenance(ctx, intent, journal, provider, check)
}
