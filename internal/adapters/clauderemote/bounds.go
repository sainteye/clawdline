// Package clauderemote controls a locally verified Claude Remote Control worker.
package clauderemote

const (
	RequestSecondsLimit  = 10
	PermissionBytesLimit = 16 << 10
	ClosePollMillisLimit = 100
	ResponseBytesLimit   = 8 << 20
)
