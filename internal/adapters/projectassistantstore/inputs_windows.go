package projectassistantstore

import "os"

func openInputWrite(root *os.Root, name string) (*os.File, error) {
	if info, err := root.Lstat(name); err == nil && (!info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0) {
		return nil, os.ErrPermission
	}
	return root.OpenFile(name, os.O_RDWR|os.O_CREATE, 0600)
}
