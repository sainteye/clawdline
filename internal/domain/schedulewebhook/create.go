package schedulewebhook

// Created is the one-time creation response. PublicURL is a credential: callers
// must save it privately and never log or retain the response in a receipt.
type Created struct {
	Schema string `json:"schema"`
	Hook   struct {
		ID        string `json:"hook_id"`
		MachineID string `json:"machine_id"`
		State     string `json:"state"`
		Revision  int64  `json:"revision"`
	} `json:"hook"`
	PublicURL       string `json:"public_url"`
	SecretAvailable bool   `json:"secret_available"`
	Duplicate       bool   `json:"duplicate"`
}
