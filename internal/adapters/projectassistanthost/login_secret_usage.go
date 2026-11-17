package projectassistanthost

// LoginSecretTargetUsage returns only a current count. It does not export any
// target addresses, profile identities or secret material.
func (h *Host) LoginSecretTargetUsage() (int, bool) {
	h.mu.Lock()
	profiles := h.profiles
	h.mu.Unlock()
	if profiles == nil {
		return 0, true
	}
	if source, ok := profiles.(interface{ SecretTargetUsage() int }); ok {
		return source.SecretTargetUsage(), true
	}
	return 0, false
}
