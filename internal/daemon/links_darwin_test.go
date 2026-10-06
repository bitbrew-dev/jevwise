//go:build darwin

package daemon

import (
	"path/filepath"
	"testing"
)

func TestPrivateHandleLinksRetainsDarwinACLRejection(t *testing.T) {
	store, file, path := linkedFixture(t)
	addACL(t, filepath.Join(path, "state.json"), "read,readattr,readextattr,readsecurity")
	if err := file.Chmod(0o600); err != nil {
		t.Fatal(err)
	}
	before, _ := file.Stat()
	if privateHandleLinks(file, 2) {
		t.Fatal("two-link count bypassed extended ACL rejection")
	}
	assertLinkedFilesUnchanged(t, store, before, "stage", "state.json")
}
