//go:build linux || darwin

package daemon

import (
	"os"
	"syscall"
	"testing"
)

type linkInfo struct {
	os.FileInfo
	stat any
}

func (i linkInfo) Sys() any { return i.stat }

func TestPrivateHandleLinksRetainsUnixPrivacy(t *testing.T) {
	store, file, _ := linkedFixture(t)
	info, _ := file.Stat()
	stat := *info.Sys().(*syscall.Stat_t)
	stat.Uid++
	if privateInfoLinks(linkInfo{info, &stat}, false, 2) || privateInfoLinks(linkInfo{info, nil}, false, 2) {
		t.Fatal("foreign or unknown owner accepted")
	}
	if err := file.Chmod(0o644); err != nil {
		t.Fatal(err)
	}
	before, _ := file.Stat()
	if privateHandleLinks(file, 2) {
		t.Fatal("two-link count bypassed private mode")
	}
	assertLinkedFilesUnchanged(t, store, before, "stage", "state.json")
}
