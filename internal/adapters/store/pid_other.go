//go:build !unix

package store

// processGone has no answer on this platform yet, and no answer is not
// "gone": an effect whose owner cannot be proved dead is left for a person
// rather than run twice.
func processGone(pid int) (gone, known bool) { return false, false }
