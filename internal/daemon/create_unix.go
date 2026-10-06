//go:build linux || darwin

package daemon

import "os"

func privateCreate(root *os.Root, name string) (*os.File, error) {
	return root.OpenFile(name, os.O_RDWR|os.O_CREATE|os.O_EXCL, 0o600)
}

func mkdirChildPrivate(root *os.Root, name string) error {
	return root.Mkdir(name, 0o700)
}
