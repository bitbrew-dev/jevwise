//go:build windows

package daemon

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
	"unsafe"

	"golang.org/x/sys/windows"
)

func creationRoot(t *testing.T) (*os.Root, string) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "runtime")
	descriptor, err := privateSecurityDescriptor()
	if err != nil {
		t.Fatal(err)
	}
	name, err := windows.UTF16PtrFromString(path)
	if err != nil {
		t.Fatal(err)
	}
	attributes := windows.SecurityAttributes{Length: uint32(unsafe.Sizeof(windows.SecurityAttributes{})), SecurityDescriptor: descriptor}
	if err := windows.CreateDirectory(name, &attributes); err != nil {
		t.Fatal(err)
	}
	root, err := os.OpenRoot(path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = root.Close() })
	return root, path
}

func assertPrivateCreation(t *testing.T, file *os.File) {
	t.Helper()
	descriptor, err := windows.GetSecurityInfo(windows.Handle(file.Fd()), windows.SE_FILE_OBJECT, windows.OWNER_SECURITY_INFORMATION|windows.DACL_SECURITY_INFORMATION)
	if err != nil {
		t.Fatal(err)
	}
	user, err := windows.GetCurrentProcessToken().GetTokenUser()
	if err != nil {
		t.Fatal(err)
	}
	owner, _, err := descriptor.Owner()
	if err != nil || owner == nil || !owner.Equals(user.User.Sid) {
		t.Fatal("created object does not explicitly belong to current user", err)
	}
	control, _, err := descriptor.Control()
	if err != nil || control&windows.SE_DACL_PROTECTED == 0 {
		t.Fatal("created DACL not protected", err)
	}
	acl, _, err := descriptor.DACL()
	if err != nil || acl == nil || acl.AceCount != 1 {
		t.Fatal("created DACL not user-only", err)
	}
	var ace *windows.ACCESS_ALLOWED_ACE
	if err := windows.GetAce(acl, 0, &ace); err != nil {
		t.Fatal(err)
	}
	sid := (*windows.SID)(unsafe.Pointer(&ace.SidStart))
	if ace.Header.AceType != windows.ACCESS_ALLOWED_ACE_TYPE || ace.Mask != 0x1f01ff || !sid.Equals(user.User.Sid) {
		t.Fatal("unexpected created access grant")
	}
}

func TestWindowsExplicitPrivateCreation(t *testing.T) {
	root, _ := creationRoot(t)
	file, err := privateCreate(root, "control.key")
	if err != nil {
		t.Fatal(err)
	}
	assertPrivateCreation(t, file)
	if _, err := file.WriteString("private key"); err != nil {
		t.Fatal(err)
	}
	if err := file.Sync(); err != nil {
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
	if data, err := root.ReadFile("control.key"); err != nil || string(data) != "private key" {
		t.Fatal("created file unusable", err)
	}
	if file, err := privateCreate(root, "control.key"); !errors.Is(err, os.ErrExist) || file != nil {
		t.Fatal("exclusive create did not preserve os.ErrExist", err)
	}
	if data, _ := root.ReadFile("control.key"); string(data) != "private key" {
		t.Fatal("existing file modified")
	}
	if err := mkdirChildPrivate(root, lockName); err != nil {
		t.Fatal(err)
	}
	directory, err := root.Open(lockName)
	if err != nil {
		t.Fatal(err)
	}
	assertPrivateCreation(t, directory)
	_ = directory.Close()
	if err := mkdirChildPrivate(root, lockName); !errors.Is(err, os.ErrExist) {
		t.Fatal("existing lock adopted", err)
	}
}

func TestWindowsCreationUsesPinnedRootAfterRename(t *testing.T) {
	root, path := creationRoot(t)
	renamed := path + "-renamed"
	if err := os.Rename(path, renamed); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(path, 0o700); err != nil {
		t.Fatal(err)
	}
	file, err := privateCreate(root, "state.json")
	if err != nil {
		t.Fatal(err)
	}
	_ = file.Close()
	if _, err := os.Stat(filepath.Join(renamed, "state.json")); err != nil {
		t.Fatal("pinned root not used", err)
	}
	if _, err := os.Stat(filepath.Join(path, "state.json")); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("replacement root mutated", err)
	}
}

func TestWindowsCreationRejectsEscapingAndInvalidNames(t *testing.T) {
	root, path := creationRoot(t)
	for _, name := range []string{"", ".", "..", `..\outside`, "../outside", `C:\outside`, `\\server\share`, "key:stream", "key\x00suffix", "CON"} {
		file, err := privateCreate(root, name)
		if err == nil || file != nil {
			t.Fatal("unsafe creation accepted", name)
		}
		if err := mkdirChildPrivate(root, name); err == nil {
			t.Fatal("unsafe directory creation accepted", name)
		}
	}
	if file, err := privateCreate(nil, "key"); err == nil || file != nil {
		t.Fatal("nil root accepted")
	}
	entries, err := os.ReadDir(path)
	if err != nil || len(entries) != 0 {
		t.Fatal("unsafe names created entries", err)
	}
	if err := root.Close(); err != nil {
		t.Fatal(err)
	}
	if file, err := privateCreate(root, "key"); err == nil || file != nil {
		t.Fatal("closed root accepted")
	}
}
