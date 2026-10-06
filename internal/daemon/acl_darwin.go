//go:build darwin

package daemon

import (
	"encoding/binary"
	"os"
	"syscall"
	"unsafe"
)

// Darwin ACL grants are independent of the POSIX mode. Reject every ACL instead
// of trying to interpret permissions, inheritance or group membership.
func noExtendedACL(file *os.File) bool {
	if file == nil {
		return false
	}
	connection, err := file.SyscallConn()
	if err != nil {
		return false
	}
	// Layout and constants: Apple's bsd/sys/attr.h. An absent ACL is an empty
	// attrreference, not a kauth_filesec blob (bsd/vfs/vfs_attrlist.c).
	attributes := struct {
		bitmapCount uint16
		reserved    uint16
		common      uint32
		volume      uint32
		directory   uint32
		file        uint32
		fork        uint32
	}{bitmapCount: 5, common: 0x00400000} // ATTR_CMN_EXTENDED_SECURITY
	var buffer [12]byte // size prefix plus one attrreference
	var syscallError syscall.Errno
	err = connection.Control(func(descriptor uintptr) {
		_, _, syscallError = syscall.Syscall6(syscall.SYS_FGETATTRLIST,
			descriptor, uintptr(unsafe.Pointer(&attributes)),
			uintptr(unsafe.Pointer(&buffer[0])), uintptr(len(buffer)),
			0x00000004, 0) // FSOPT_REPORT_FULLSIZE detects truncated ACL data.
	})
	return err == nil && syscallError == 0 && emptySecurityReference(buffer)
}

func emptySecurityReference(buffer [12]byte) bool {
	// Both supported Darwin architectures use little-endian integers. Any
	// malformed, nonempty, truncated or unfamiliar result fails closed.
	return binary.LittleEndian.Uint32(buffer[0:4]) == uint32(len(buffer)) &&
		binary.LittleEndian.Uint32(buffer[4:8]) == 8 &&
		binary.LittleEndian.Uint32(buffer[8:12]) == 0
}
