//go:build windows

package daemon

import (
	"os"
	"unsafe"

	"golang.org/x/sys/windows"
)

func privateReadFlags() int { return os.O_RDONLY }

func privateHandle(file *os.File, directory, protected bool) bool {
	if file == nil {
		return false
	}
	raw, err := file.SyscallConn()
	if err != nil {
		return false
	}
	valid := false
	err = raw.Control(func(fd uintptr) {
		valid = privateWindowsHandle(windows.Handle(fd), directory, protected)
	})
	return err == nil && valid
}

func privateWindowsHandle(handle windows.Handle, directory, protected bool) bool {
	var info windows.ByHandleFileInformation
	if windows.GetFileInformationByHandle(handle, &info) != nil || info.FileAttributes&windows.FILE_ATTRIBUTE_REPARSE_POINT != 0 {
		return false
	}
	if (info.FileAttributes&windows.FILE_ATTRIBUTE_DIRECTORY != 0) != directory || (!directory && info.NumberOfLinks != 1) {
		return false
	}
	user, err := windows.GetCurrentProcessToken().GetTokenUser()
	if err != nil {
		return false
	}
	descriptor, err := windows.GetSecurityInfo(handle, windows.SE_FILE_OBJECT, windows.OWNER_SECURITY_INFORMATION|windows.DACL_SECURITY_INFORMATION)
	return err == nil && privateDescriptor(descriptor, user.User.Sid, directory, protected)
}

func privateDescriptor(descriptor *windows.SECURITY_DESCRIPTOR, user *windows.SID, directory, protected bool) bool {
	if descriptor == nil || user == nil {
		return false
	}
	owner, _, err := descriptor.Owner()
	if err != nil || owner == nil || !owner.Equals(user) {
		return false
	}
	control, _, err := descriptor.Control()
	if err != nil || (protected && control&windows.SE_DACL_PROTECTED == 0) {
		return false
	}
	acl, _, err := descriptor.DACL()
	if err != nil || acl == nil || acl.AceCount != 1 {
		return false
	}
	var ace *windows.ACCESS_ALLOWED_ACE
	if windows.GetAce(acl, 0, &ace) != nil || ace == nil || ace.Header.AceType != windows.ACCESS_ALLOWED_ACE_TYPE || int(ace.Header.AceSize) < int(unsafe.Offsetof(ace.SidStart))+8 {
		return false
	}
	// Reject inherit-only and no-propagate entries; directory permissions must
	// protect future descendants as well as the current directory.
	if ace.Header.AceFlags&(windows.INHERIT_ONLY_ACE|windows.NO_PROPAGATE_INHERIT_ACE) != 0 {
		return false
	}
	inheritance := uint8(windows.OBJECT_INHERIT_ACE | windows.CONTAINER_INHERIT_ACE)
	if directory && ace.Header.AceFlags&inheritance != inheritance {
		return false
	}
	sid := (*windows.SID)(unsafe.Pointer(&ace.SidStart))
	const fileAllAccess = 0x1f01ff
	return sid.IsValid() && int(ace.Header.AceSize) >= int(unsafe.Offsetof(ace.SidStart))+sid.Len() && sid.Equals(user) && ace.Mask == fileAllAccess
}
