package buildinfo

import (
	"runtime"
	"strings"
	"testing"
)

func TestDefaultAndLinkedMetadataFormat(t *testing.T) {
	beforeVersion, beforeCommit, beforeDate := Version, Commit, Date
	t.Cleanup(func() { Version, Commit, Date = beforeVersion, beforeCommit, beforeDate })
	if Version != "dev" || Commit != "unknown" || Date != "unknown" || UpdateStamp != "" {
		t.Fatal("incorrect default metadata")
	}
	Version, Commit, Date = "v1.2.3", "abc123", "2026-10-07T00:00:00Z"
	want := "jev v1.2.3\ncommit: abc123\nbuilt: 2026-10-07T00:00:00Z\ngo: " + runtime.Version() + "\nplatform: " + runtime.GOOS + "/" + runtime.GOARCH + "\n"
	if String() != want || strings.Contains(String(), "unknown") {
		t.Fatal("metadata not used by formatter")
	}
}

func TestUpdateStampConsistency(t *testing.T) {
	beforeVersion, beforeStamp := Version, UpdateStamp
	t.Cleanup(func() { Version, UpdateStamp = beforeVersion, beforeStamp })
	Version = "v1.2.3"
	for _, tc := range []struct{ stamp, want string }{
		{"", "jev v1.2.3\n"},
		{UpdateStampPrefix + Version + UpdateStampSuffix, "jev v1.2.3\n"},
		{UpdateStampPrefix + "v2.0.0" + UpdateStampSuffix, "jev unknown\n"},
	} {
		UpdateStamp = tc.stamp
		if !strings.HasPrefix(String(), tc.want) {
			t.Fatal("inconsistent update stamp", String())
		}
	}
}
