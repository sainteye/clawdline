package cloudops

// A paired browser can ask a machine it already controls to carry a pairing
// offer to a second machine. The offer reaches the helper inside the encrypted
// Cloud command; the Cloud API never receives the plaintext. The helper's own
// pairing route checks the offer's account and starts only the fixed task.
func init() {
	register(
		op{name: "pair-agent-start"},
		op{name: "pair-agent-status", read: true},
	)
}

// Browser invitations admit a browser with no Clawdline account to this
// machine alone (docs/browser-invite.md). A paired device starts one, reads
// it, compares the digits and decides; the route answers only this machine,
// and an invited browser may use none of these words (machineOnlyWords).
func init() {
	register(
		op{name: "browser-invite-start"},
		op{name: "browser-invite-list", read: true},
		op{name: "browser-invite-status", read: true},
		op{name: "browser-invite-decide"},
		op{name: "browser-invite-cancel"},
		op{name: "browser-invite-revoke"},
	)
}

// The Devices page's directly signed-in devices, read and revoked from Cloud
// as from this machine's own window. Both run as this machine.
func init() {
	register(
		op{name: "direct-devices", read: true},
		op{name: "direct-device-revoke"},
	)
}
