//go:build !linux && !darwin && !windows

package daemon

import "os"

func privateCreate(*os.Root, string) (*os.File, error) { return nil, storageSupported() }
func mkdirChildPrivate(*os.Root, string) error         { return storageSupported() }
