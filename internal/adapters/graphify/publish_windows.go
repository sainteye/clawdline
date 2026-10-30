package graphify

import "os"

// Windows directory rename refuses an existing destination, including an empty
// directory; it does not carry POSIX rename's replace-empty-directory behavior.
func publishDirectory(root *os.Root, from, to string) error { return root.Rename(from, to) }
