//go:build windows

package daemon

import (
	"os"
	"path/filepath"
	"testing"

	"golang.org/x/sys/windows"
)

func TestPublicationTransientWindowsDACLChangePreservesAliases(t *testing.T) {
	metadata, directory := transientMetadata(t)
	descriptor, err := windows.SecurityDescriptorFromString("D:P(A;;FA;;;WD)")
	if err != nil {
		t.Fatal(err)
	}
	acl, _, err := descriptor.DACL()
	if err != nil {
		t.Fatal(err)
	}
	if err := windows.SetNamedSecurityInfo(filepath.Join(directory, stageName), windows.SE_FILE_OBJECT, windows.DACL_SECURITY_INFORMATION|windows.PROTECTED_DACL_SECURITY_INFORMATION, nil, nil, acl, nil); err != nil {
		t.Fatal(err)
	}
	publication := &Publication{files: []*ownedFile{metadata}}
	if err := publication.Close(); err == nil {
		t.Fatal("two-link DACL change accepted")
	}
	for _, name := range []string{stageName, "state.json"} {
		if _, err := os.Stat(filepath.Join(directory, name)); err != nil {
			t.Fatal("unsafe alias removed")
		}
	}
}
