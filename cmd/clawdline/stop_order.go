package main

// stopServing is the order every path that stops serving uses. Owned Gemini
// and Grok workers run under the Cloud context; their hosts must stop first so
// Shutdown marks a running turn for the restart continuation before that
// context kills its worker (2026-11-11, docs/antigravity-acp-client.md).
func stopServing(stopOwnedHosts, stopCloud, stopCodeGraph func()) {
	stopOwnedHosts()
	stopCloud()
	stopCodeGraph()
}
