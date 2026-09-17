//go:build !unix

package cloudkeys

// noFollow has no portable spelling here. What refuses a link on these
// platforms is the Lstat before the open and the same-file check after it.
const noFollow = 0

func isLinkRefusal(error) bool { return false }
