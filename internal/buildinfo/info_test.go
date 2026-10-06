package buildinfo

import (
	"runtime"
	"strings"
	"testing"
)

func TestDefaultAndLinkedMetadataFormat(t *testing.T) {
	beforeVersion, beforeCommit, beforeDate := Version, Commit, Date
	t.Cleanup(func() { Version, Commit, Date = beforeVersion, beforeCommit, beforeDate })
	if Version != "dev" || Commit != "unknown" || Date != "unknown" {
		t.Fatal("incorrect default metadata")
	}
	Version, Commit, Date = "v1.2.3", "abc123", "2026-10-07T00:00:00Z"
	want := "jev v1.2.3\ncommit: abc123\nbuilt: 2026-10-07T00:00:00Z\ngo: " + runtime.Version() + "\nplatform: " + runtime.GOOS + "/" + runtime.GOARCH + "\n"
	if String() != want || strings.Contains(String(), "unknown") {
		t.Fatal("metadata not used by formatter")
	}
}
