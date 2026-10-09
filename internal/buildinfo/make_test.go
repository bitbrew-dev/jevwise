package buildinfo

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestMakeExactCleanTagAndWindowsOutput(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Unix make recipe tested on native Unix")
	}
	makePath, err := exec.LookPath("make")
	if err != nil {
		t.Skip("make unavailable")
	}
	bin := t.TempDir()
	fake := "#!/bin/sh\ncase \"$1\" in\nstatus) [ \"$MOCK_DIRTY\" != 1 ] || echo ' M tracked' ;;\ndescribe) [ \"$2\" = --exact-match ] || exit 1; [ \"$MOCK_TAG\" != missing ] || exit 1; echo \"$MOCK_TAG\" ;;\nrev-parse) echo abc123 ;;\nesac\n"
	if err := os.WriteFile(filepath.Join(bin, "git"), []byte(fake), 0700); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct{ dirty, tag, explicit, want string }{
		{"0", "v1.2.3", "", "v1.2.3"}, {"1", "v1.2.3", "", "dev"}, {"0", "missing", "", "dev"}, {"1", "missing", "custom", "custom"},
	} {
		args := []string{"-n", "-f", "../../makefile", "build-platform", "GOOS=windows", "GOARCH=arm64", "DirName=custom-output", "CORES=1", "BuildTime=stamp", "BuildCommit=abc123"}
		if tc.explicit != "" {
			args = append(args, "VERSION="+tc.explicit)
		}
		cmd := exec.Command(makePath, args...)
		for _, entry := range os.Environ() {
			if !strings.HasPrefix(entry, "VERSION=") {
				cmd.Env = append(cmd.Env, entry)
			}
		}
		cmd.Env = append(cmd.Env, "PATH="+bin+string(os.PathListSeparator)+os.Getenv("PATH"), "MOCK_DIRTY="+tc.dirty, "MOCK_TAG="+tc.tag)
		output, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("make failed: %s", output)
		}
		text := string(output)
		for _, want := range []string{"go build -trimpath", "mkdir -p \"custom-output\"", "jevwise-windows-arm64.exe", "github.com/bitbrew-dev/jevwise/internal/buildinfo.Version=" + tc.want, "buildinfo.Commit=abc123", "buildinfo.Date=stamp", "buildinfo.UpdateStamp=" + UpdateStampPrefix + tc.want + UpdateStampSuffix} {
			if !strings.Contains(text, want) {
				t.Errorf("make output missing %q: %s", want, text)
			}
		}
		if strings.Contains(text, "jev-windows-arm64.exe") || strings.Contains(text, "chmod") || strings.Contains(text, "/pkg/config") {
			t.Fatal("obsolete or Windows-incompatible build recipe")
		}
	}
}
