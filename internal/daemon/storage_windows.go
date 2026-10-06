//go:build windows

package daemon

import (
	"os"
	"syscall"
	"unsafe"

	"golang.org/x/sys/windows"
)

func storageSupported() error { return nil }

// The leaf is private from creation, even beneath a permissive trusted parent.
func mkdirPrivate(path string) error {
	descriptor, err := privateSecurityDescriptor()
	if err != nil {
		return err
	}
	name, err := windows.UTF16PtrFromString(path)
	if err != nil {
		return err
	}
	attributes := windows.SecurityAttributes{Length: uint32(unsafe.Sizeof(windows.SecurityAttributes{})), SecurityDescriptor: descriptor}
	return windows.CreateDirectory(name, &attributes)
}

// Metadata guards precede opening. Ownership and DACL checks use opened handles.
func privateInfo(info os.FileInfo, directory bool) bool {
	if info == nil || info.Mode()&os.ModeSymlink != 0 {
		return false
	}
	data, ok := info.Sys().(*syscall.Win32FileAttributeData)
	if !ok || data.FileAttributes&windows.FILE_ATTRIBUTE_REPARSE_POINT != 0 {
		return false
	}
	if directory {
		return info.IsDir()
	}
	return info.Mode().IsRegular()
}
