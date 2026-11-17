package projectassistanthost

import "context"

// An unconfigured namespace preserves harmless preferences without claiming
// account authorization. It is never promoted into a configured account.
func (h *Host) networkAccount(ctx context.Context) (string, error) {
	if h.o.NetworkAccount != nil {
		return h.o.NetworkAccount(ctx)
	}
	if h.o.Network != nil {
		return h.o.Network.AccountID(), nil
	}
	return "unconfigured:" + h.o.MachineID, nil
}
