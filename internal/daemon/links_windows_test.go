//go:build windows

package daemon

import (
	"path/filepath"
	"testing"

	"golang.org/x/sys/windows"
)

func TestPrivateHandleLinksRetainsWindowsDACLAndOwner(t *testing.T) {
	store, file, path := linkedFixture(t)
	user, err := windows.GetCurrentProcessToken().GetTokenUser()
	if err != nil {
		t.Fatal(err)
	}
	wrongOwner, err := windows.SecurityDescriptorFromString("O:WDD:P(A;;FA;;;" + user.User.Sid.String() + ")")
	if err != nil {
		t.Fatal(err)
	}
	if privateDescriptor(wrongOwner, user.User.Sid, false, false) {
		t.Fatal("foreign Windows owner accepted")
	}
	broad, err := windows.SecurityDescriptorFromString("O:" + user.User.Sid.String() + "D:P(A;;FA;;;WD)")
	if err != nil {
		t.Fatal(err)
	}
	acl, _, err := broad.DACL()
	if err != nil {
		t.Fatal(err)
	}
	// The fixture acquires DACL access independently from production RW handles.
	if err := windows.SetNamedSecurityInfo(filepath.Join(path, "stage"), windows.SE_FILE_OBJECT, windows.DACL_SECURITY_INFORMATION|windows.PROTECTED_DACL_SECURITY_INFORMATION, nil, nil, acl, nil); err != nil {
		t.Fatal(err)
	}
	before, _ := file.Stat()
	if privateHandleLinks(file, 2) {
		t.Fatal("two-link count bypassed exact user-only DACL")
	}
	assertLinkedFilesUnchanged(t, store, before, "stage", "state.json")
}
