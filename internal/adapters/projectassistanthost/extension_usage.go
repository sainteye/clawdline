package projectassistanthost

func (h *Host) ConnectorSecretTargetUsage() (int, bool) {
	if i, ok := h.o.Extensions.(interface{ ConnectorSecretTargetUsage() (int, bool) }); ok {
		return i.ConnectorSecretTargetUsage()
	}
	return 0, false
}
