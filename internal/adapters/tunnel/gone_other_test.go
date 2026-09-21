//go:build !unix

package tunnel

// processGone has no answer on this platform yet, and no answer is neither
// "gone" nor "running": gone fails the test that asks. Nothing asks today —
// every test that would runs the fake cloudflared, which skips here first.
func processGone(pid int) (gone, known bool) { return false, false }
