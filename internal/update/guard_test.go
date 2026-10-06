package update

import (
	"bytes"
	"context"
	"crypto/sha256"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"runtime/debug"
	"strings"
	"testing"

	"github.com/bitbrew-dev/jevwise/internal/buildinfo"
)

func versionStamp(tag string) string {
	return buildinfo.UpdateStampPrefix + tag + buildinfo.UpdateStampSuffix
}

func TestExecutableStamp(t *testing.T) {
	max := "v18446744073709551615.18446744073709551615.18446744073709551615"
	for _, tag := range []string{"v0.0.0", "v1.2.3", max} {
		data := buildinfo.UpdateStampPrefix + " rodata " + versionStamp("dev") + versionStamp(tag) + buildinfo.UpdateStampSuffix
		if got, err := executableStamp([]byte(data)); err != nil || got != tag {
			t.Fatalf("stamp=%q,%v", got, err)
		}
	}
	for _, data := range []string{"", "dev", buildinfo.UpdateStampPrefix, versionStamp("dev"), versionStamp("v01.2.3"), versionStamp("v1.2.3-rc1"), versionStamp("v18446744073709551616.0.0"), buildinfo.UpdateStampPrefix + "v1.2.3", versionStamp("v1.2.3") + versionStamp("v1.2.3"), versionStamp("v1.2.3") + versionStamp("v2.0.0"), versionStamp(strings.Repeat("9", 100))} {
		if _, err := executableStamp([]byte(data)); err == nil {
			t.Fatalf("accepted %q", data)
		}
	}
}

func TestExecutableMetadata(t *testing.T) {
	makeInfo := func(os, arch string) *debug.BuildInfo {
		return &debug.BuildInfo{Path: "github.com/bitbrew-dev/jevwise/cmd", Main: debug.Module{Path: "github.com/bitbrew-dev/jevwise"}, Settings: []debug.BuildSetting{{Key: "GOOS", Value: os}, {Key: "GOARCH", Value: arch}}}
	}
	for _, os := range []string{"linux", "darwin", "windows"} {
		for _, arch := range []string{"amd64", "arm64"} {
			if err := validateExecutableMetadata(makeInfo(os, arch), os, arch); err != nil {
				t.Fatal(err)
			}
		}
	}
	for _, change := range []func(*debug.BuildInfo){
		func(i *debug.BuildInfo) { i.Path = "private" }, func(i *debug.BuildInfo) { i.Main.Path = "private" },
		func(i *debug.BuildInfo) { i.Main.Replace = &debug.Module{Path: "private"} },
		func(i *debug.BuildInfo) { i.Settings = i.Settings[:1] }, func(i *debug.BuildInfo) { i.Settings = i.Settings[1:] },
		func(i *debug.BuildInfo) { i.Settings[0].Value = "windows" }, func(i *debug.BuildInfo) { i.Settings[1].Value = "arm64" },
		func(i *debug.BuildInfo) { i.Settings = append(i.Settings, i.Settings[0]) }, func(i *debug.BuildInfo) { i.Settings = append(i.Settings, i.Settings[1]) },
	} {
		info := makeInfo("linux", "amd64")
		change(info)
		if err := validateExecutableMetadata(info, "linux", "amd64"); err == nil {
			t.Fatal("invalid metadata accepted")
		}
	}
	if validateExecutableMetadata(nil, "linux", "amd64") == nil || validateExecutableMetadata(makeInfo("freebsd", "386"), "freebsd", "386") == nil {
		t.Fatal("invalid identity accepted")
	}
}

type readAtFunc func([]byte, int64) (int, error)

func (f readAtFunc) ReadAt(p []byte, offset int64) (int, error) { return f(p, offset) }

func TestInstalledReadBoundaries(t *testing.T) {
	secret := errors.New("private executable path")
	for _, size := range []int64{0, -1, binaryLimit + 1} {
		reader := readAtFunc(func([]byte, int64) (int, error) { t.Fatal("invalid size read"); return 0, nil })
		if err := readInstalled(context.Background(), reader, size, "v1.2.3", "linux", "amd64"); err == nil {
			t.Fatal("size accepted")
		}
	}
	for _, cancelRead := range []bool{false, true} {
		ctx, cancel := context.WithCancel(context.Background())
		reader := readAtFunc(func([]byte, int64) (int, error) {
			if cancelRead {
				cancel()
			}
			return 0, secret
		})
		err := readInstalled(ctx, reader, 10, "v1.2.3", "linux", "amd64")
		cancel()
		want := error(secret)
		if cancelRead {
			want = context.Canceled
		}
		if !errors.Is(err, want) || strings.Contains(err.Error(), "private") {
			t.Fatalf("cause=%v", err)
		}
	}
	if err := readInstalled(context.Background(), bytes.NewReader([]byte("x")), 2, "v1.2.3", "linux", "amd64"); !errors.Is(err, io.ErrUnexpectedEOF) {
		t.Fatalf("short read: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	reader := readAtFunc(func([]byte, int64) (int, error) { t.Fatal("canceled read dispatched"); return 0, nil })
	if err := readInstalled(ctx, reader, 10, "v1.2.3", "linux", "amd64"); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancellation=%v", err)
	}
	if readInstalled(nil, reader, 10, "v1.2.3", "linux", "amd64") == nil || readInstalled(context.Background(), nil, 10, "v1.2.3", "linux", "amd64") == nil {
		t.Fatal("nil input accepted")
	}
}

func TestNativeTrimmedExecutableGuard(t *testing.T) {
	path := filepath.Join(t.TempDir(), "jev-fixture")
	prefix := "github.com/bitbrew-dev/jevwise/internal/buildinfo."
	flags := "-s -w -X " + prefix + "Version=v1.2.3 -X " + prefix + "UpdateStamp=" + versionStamp("v1.2.3")
	command := exec.Command("go", "build", "-mod=readonly", "-trimpath", "-ldflags="+flags, "-o", path, "./cmd")
	command.Dir = "../.."
	command.Env = append(os.Environ(), "GOPROXY=off", "GOFLAGS=-p=4")
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("fixture build: %v\n%s", err, output)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	reader := bytes.NewReader(data)
	if err := readInstalled(ctx, reader, int64(len(data)), "v1.2.3", runtime.GOOS, runtime.GOARCH); err != nil {
		t.Fatalf("trimmed executable refused: %v (cause %v)", err, errors.Unwrap(err))
	}
	if err := readInstalled(ctx, reader, int64(len(data)), "v1.2.3", "unsupported", runtime.GOARCH); err == nil {
		t.Fatal("actual executable platform mismatch accepted")
	}
	if reader.Len() != len(data) {
		t.Fatal("caller reader offset changed")
	}
	// A cooperative update before baseline capture must not permit a downgrade.
	if err := readInstalled(ctx, reader, int64(len(data)), "v0.9.0", runtime.GOOS, runtime.GOARCH); err == nil {
		t.Fatal("stale running version accepted")
	}
	binary := &Binary{data: data, digest: sha256.Sum256(data), tag: "v1.2.3", goos: runtime.GOOS, goarch: runtime.GOARCH}
	if err := validateBinary(ctx, binary, "v1.2.2", runtime.GOOS, runtime.GOARCH); err != nil {
		t.Fatalf("candidate refused: %v", err)
	}
	for _, current := range []string{"v1.2.3", "v2.0.0", "dev", "unknown"} {
		if validateBinary(ctx, binary, current, runtime.GOOS, runtime.GOARCH) == nil || (current != binary.tag && readInstalled(ctx, reader, int64(len(data)), current, runtime.GOOS, runtime.GOARCH) == nil) {
			t.Fatalf("unsafe current %q accepted", current)
		}
	}
	for _, change := range []func(*Binary){
		func(b *Binary) { b.digest[0] ^= 1 }, func(b *Binary) { b.tag = "v2.0.0" },
		func(b *Binary) { b.goos = "private" }, func(b *Binary) { b.goarch = "private" },
		func(b *Binary) { b.data = []byte("private malformed binary"); b.digest = sha256.Sum256(b.data) },
		func(b *Binary) {
			b.data = bytes.Replace(data, []byte(versionStamp("v1.2.3")), []byte(versionStamp("v1.2.4")), 1)
			b.digest = sha256.Sum256(b.data)
		},
	} {
		candidate := *binary
		change(&candidate)
		if err := validateBinary(ctx, &candidate, "v1.2.2", runtime.GOOS, runtime.GOARCH); err == nil || strings.Contains(err.Error(), "private") {
			t.Fatalf("invalid candidate: %v", err)
		}
	}
	oversized := &Binary{data: make([]byte, binaryLimit+1)}
	if validateBinary(ctx, oversized, "v1.2.2", runtime.GOOS, runtime.GOARCH) == nil {
		t.Fatal("oversized candidate accepted")
	}
	canceled, cancel := context.WithCancel(ctx)
	cancel()
	if err := validateBinary(canceled, binary, "v1.2.2", runtime.GOOS, runtime.GOARCH); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancel=%v", err)
	}
	if validateBinary(nil, binary, "v1.2.2", runtime.GOOS, runtime.GOARCH) == nil || validateBinary(ctx, nil, "v1.2.2", runtime.GOOS, runtime.GOARCH) == nil || validateBinary(ctx, &Binary{}, "v1.2.2", runtime.GOOS, runtime.GOARCH) == nil {
		t.Fatal("nil/zero candidate accepted")
	}
}

func TestMakeTrimmedExecutableGuard(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Unix make recipe tested on native Unix")
	}
	makePath, err := exec.LookPath("make")
	if err != nil {
		t.Skip("make unavailable")
	}
	directory := t.TempDir()
	command := exec.Command(makePath, "build-platform", "VERSION=v1.2.3", "GOOS="+runtime.GOOS, "GOARCH="+runtime.GOARCH, "CORES=4", "DirName="+directory)
	command.Dir = "../.."
	command.Env = append(os.Environ(), "GOPROXY=off", "GOFLAGS=-p=4 -mod=readonly", "CGO_ENABLED=0")
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("Make fixture build: %v\n%s", err, output)
	}
	data, err := os.ReadFile(filepath.Join(directory, "jev-"+runtime.GOOS+"-"+runtime.GOARCH))
	if err != nil {
		t.Fatal(err)
	}
	if err := readInstalled(context.Background(), bytes.NewReader(data), int64(len(data)), "v1.2.3", runtime.GOOS, runtime.GOARCH); err != nil {
		t.Fatalf("Make executable refused: %v (cause %v)", err, errors.Unwrap(err))
	}
}
