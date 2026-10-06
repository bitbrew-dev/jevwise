//go:build windows

package daemon

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"unsafe"

	"golang.org/x/sys/windows"
)

// Explicit ownership avoids an elevated token's Administrators default owner.
// Protection and inheritance make every created object private immediately.
func privateSecurityDescriptor() (*windows.SECURITY_DESCRIPTOR, error) {
	user, err := windows.GetCurrentProcessToken().GetTokenUser()
	if err != nil {
		return nil, err
	}
	sid := user.User.Sid.String()
	return windows.SecurityDescriptorFromString("O:" + sid + "D:P(A;OICI;FA;;;" + sid + ")")
}

func privateCreate(root *os.Root, name string) (*os.File, error) {
	return createPrivateObject(root, name, false)
}

func mkdirChildPrivate(root *os.Root, name string) error {
	file, err := createPrivateObject(root, name, true)
	if err != nil {
		return err
	}
	return file.Close()
}

// All creation is exclusive and relative to a pinned directory handle. Names
// are single components, never alternate streams, reparse paths or absolute paths.
func createPrivateObject(root *os.Root, name string, directory bool) (*os.File, error) {
	if root == nil || len(name) == 0 || len(name) > 255 || name == "." || !filepath.IsLocal(name) || strings.ContainsAny(name, `/\:`) {
		return nil, errors.New("invalid runtime object name")
	}
	descriptor, err := privateSecurityDescriptor()
	if err != nil {
		return nil, err
	}
	objectName, err := windows.NewNTUnicodeString(name)
	if err != nil {
		return nil, err
	}
	parent, err := root.Open(".")
	if err != nil {
		return nil, err
	}
	raw, err := parent.SyscallConn()
	if err != nil {
		return nil, errors.Join(err, parent.Close())
	}
	access := uint32(windows.GENERIC_READ | windows.GENERIC_WRITE | windows.SYNCHRONIZE)
	options := uint32(windows.FILE_NON_DIRECTORY_FILE)
	if directory {
		options = windows.FILE_DIRECTORY_FILE
		access = windows.FILE_LIST_DIRECTORY | windows.FILE_TRAVERSE | windows.FILE_READ_ATTRIBUTES | windows.READ_CONTROL | windows.SYNCHRONIZE
	}
	options |= windows.FILE_SYNCHRONOUS_IO_NONALERT | windows.FILE_OPEN_REPARSE_POINT
	var handle windows.Handle
	var nativeErr error
	controlErr := raw.Control(func(fd uintptr) {
		attributes := windows.OBJECT_ATTRIBUTES{
			Length:        uint32(unsafe.Sizeof(windows.OBJECT_ATTRIBUTES{})),
			RootDirectory: windows.Handle(fd), ObjectName: objectName,
			Attributes:         windows.OBJ_CASE_INSENSITIVE | windows.OBJ_DONT_REPARSE,
			SecurityDescriptor: descriptor,
		}
		var status windows.IO_STATUS_BLOCK
		nativeErr = windows.NtCreateFile(&handle, access,
			&attributes, &status, nil, windows.FILE_ATTRIBUTE_NORMAL,
			windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE|windows.FILE_SHARE_DELETE,
			windows.FILE_CREATE, options, 0, 0)
	})
	// Convert NT collision codes before joining so os.ErrExist still matches.
	if status, ok := nativeErr.(windows.NTStatus); ok {
		nativeErr = status.Errno()
	}
	err = errors.Join(controlErr, nativeErr, parent.Close())
	if err != nil {
		if handle != 0 && handle != windows.InvalidHandle {
			_ = windows.CloseHandle(handle)
		}

		return nil, err
	}
	file := os.NewFile(uintptr(handle), name)
	if file == nil {
		_ = windows.CloseHandle(handle)
		return nil, errors.New("runtime object handle unavailable")
	}
	return file, nil
}
