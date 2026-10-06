//go:build windows

package daemon

import (
	"errors"
	"os"
	"path/filepath"
	"syscall"
	"testing"
	"unsafe"

	"golang.org/x/sys/windows"
)

func currentSID(t *testing.T) *windows.SID {
	t.Helper()
	user, err := windows.GetCurrentProcessToken().GetTokenUser()
	if err != nil {
		t.Fatal(err)
	}
	return user.User.Sid
}

func descriptorFixture(t *testing.T, sddl string) *windows.SECURITY_DESCRIPTOR {
	t.Helper()
	descriptor, err := windows.SecurityDescriptorFromString(sddl)
	if err != nil {
		t.Fatal(err)
	}
	return descriptor
}

func TestWindowsDescriptorRejectsUnsafeACLs(t *testing.T) {
	user := currentSID(t)
	sid := user.String()
	owner := "O:" + sid
	for _, suffix := range []string{
		"D:NO_ACCESS_CONTROL", "D:P", "D:P(A;OICI;FA;;;WD)",
		"D:P(A;OICI;FA;;;" + sid + ")(A;OICI;GR;;;WD)",
		"D:P(A;OICI;GR;;;" + sid + ")", "D:(A;OICI;FA;;;" + sid + ")",
		"D:P(A;OI;FA;;;" + sid + ")", "D:P(A;OICIIO;FA;;;" + sid + ")",
		"D:P(A;OICINP;FA;;;" + sid + ")", "D:P(D;OICI;FA;;;" + sid + ")",
	} {
		t.Run(suffix, func(t *testing.T) {
			if privateDescriptor(descriptorFixture(t, owner+suffix), user, true, true) {
				t.Fatal("unsafe ACL accepted")
			}
		})
	}
	if privateDescriptor(descriptorFixture(t, "O:SYD:P(A;OICI;FA;;;"+sid+")"), user, true, true) {
		t.Fatal("foreign owner accepted")
	}
	if !privateDescriptor(descriptorFixture(t, owner+"D:P(A;OICI;FA;;;"+sid+")"), user, true, true) {
		t.Fatal("private protected directory rejected")
	}
	inherited := descriptorFixture(t, owner+"D:(A;OICIID;FA;;;"+sid+")")
	if !privateDescriptor(inherited, user, true, false) || privateDescriptor(inherited, user, true, true) {
		t.Fatal("inherited/protected ACL distinction lost")
	}
}

func TestWindowsPrivateStoreAndInheritedFiles(t *testing.T) {
	path := filepath.Join(t.TempDir(), "runtime")
	store, err := OpenStore(path)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	file, err := store.root.Open(".")
	if err != nil {
		t.Fatal(err)
	}
	if !privateHandle(file, true, true) {
		t.Fatal("root not privately protected")
	}
	_ = file.Close()
	lease, err := store.Acquire()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.Acquire(); !errors.Is(err, ErrLocked) {
		t.Fatal("duplicate lock accepted", err)
	}
	if _, err := (&Store{lease.root}).directoryInfo(false); err != nil {
		t.Fatal("private inherited lock rejected", err)
	}
	if err := lease.Close(); err != nil {
		t.Fatal(err)
	}
	if err := store.Create("control.key", []byte("private management key")); err != nil {
		t.Fatal(err)
	}
	file, err = store.root.Open("control.key")
	if err != nil {
		t.Fatal(err)
	}
	if !privateHandle(file, false, false) {
		t.Fatal("created file inherited unsafe ACL")
	}
	_ = file.Close()
	if data, err := store.Read("control.key", 128); err != nil || string(data) != "private management key" {
		t.Fatal("private read failed", err)
	}
	if err := os.Link(filepath.Join(path, "control.key"), filepath.Join(path, "alias")); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Read("control.key", 128); err == nil {
		t.Fatal("hardlinked key accepted")
	}
}

func TestWindowsExistingPermissiveRootIsNotRepaired(t *testing.T) {
	path := filepath.Join(t.TempDir(), "runtime")
	name, err := windows.UTF16PtrFromString(path)
	if err != nil {
		t.Fatal(err)
	}
	descriptor := descriptorFixture(t, "O:"+currentSID(t).String()+"D:P(A;OICI;FA;;;WD)")
	if err := windows.CreateDirectory(name, &windows.SecurityAttributes{Length: uint32(unsafe.Sizeof(windows.SecurityAttributes{})), SecurityDescriptor: descriptor}); err != nil {
		t.Fatal(err)
	}
	before, err := os.Lstat(path)
	if err != nil {
		t.Fatal(err)
	}
	if store, err := OpenStore(path); err == nil || store != nil {
		t.Fatal("permissive root accepted")
	}
	after, err := os.Lstat(path)
	if err != nil || !os.SameFile(before, after) {
		t.Fatal("existing root changed", err)
	}
	entries, err := os.ReadDir(path)
	if err != nil || len(entries) != 0 {
		t.Fatal("unsafe root mutated", err)
	}
	file, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	if privateHandle(file, true, true) {
		t.Fatal("unsafe ACL repaired")
	}
}

func TestWindowsStoreRechecksRootAndFileACLs(t *testing.T) {
	path := filepath.Join(t.TempDir(), "runtime")
	store, err := OpenStore(path)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	if err := store.Create("control.key", []byte("key")); err != nil {
		t.Fatal(err)
	}
	descriptor := descriptorFixture(t, "D:P(A;OICI;FA;;;WD)")
	acl, _, err := descriptor.DACL()
	if err != nil {
		t.Fatal(err)
	}
	flags := windows.SECURITY_INFORMATION(windows.DACL_SECURITY_INFORMATION | windows.PROTECTED_DACL_SECURITY_INFORMATION)
	key := filepath.Join(path, "control.key")
	if err := windows.SetNamedSecurityInfo(key, windows.SE_FILE_OBJECT, flags, nil, nil, acl, nil); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Read("control.key", 128); err == nil {
		t.Fatal("changed key ACL accepted")
	}
	if err := windows.SetNamedSecurityInfo(path, windows.SE_FILE_OBJECT, flags, nil, nil, acl, nil); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Acquire(); err == nil {
		t.Fatal("changed root ACL accepted")
	}
	if _, err := os.Stat(filepath.Join(path, lockName)); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("unsafe root mutated", err)
	}
}

type windowsMetadata struct {
	os.FileInfo
	data any
}

func (i windowsMetadata) Sys() any { return i.data }

func TestWindowsRejectsReparseAndUnknownMetadata(t *testing.T) {
	path := filepath.Join(t.TempDir(), "file")
	if err := os.WriteFile(path, []byte("key"), 0o600); err != nil {
		t.Fatal(err)
	}
	info, err := os.Lstat(path)
	if err != nil {
		t.Fatal(err)
	}
	data := *info.Sys().(*syscall.Win32FileAttributeData)
	data.FileAttributes |= windows.FILE_ATTRIBUTE_REPARSE_POINT
	if privateInfo(windowsMetadata{info, &data}, false) || privateInfo(windowsMetadata{info, nil}, false) {
		t.Fatal("reparse or unknown metadata accepted")
	}
	if privateHandle(nil, false, false) || privateDescriptor(nil, currentSID(t), false, false) {
		t.Fatal("unavailable metadata accepted")
	}
	file, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	_ = file.Close()
	if privateHandle(file, false, false) {
		t.Fatal("closed file accepted")
	}
}
