//go:build linux || darwin

package update

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestReplaceRefusesSameMetadataInodeExchange(t *testing.T) {
	original, binary := replacementFixture(t)
	directory, path := replacementTarget(t, original)
	before, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	exchange := filepath.Join(directory, "exchange")
	if err := os.WriteFile(exchange, original, before.Mode().Perm()); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(exchange, before.ModTime(), before.ModTime()); err != nil {
		t.Fatal(err)
	}
	swapped, err := os.Stat(exchange)
	if err != nil || os.SameFile(before, swapped) || before.Mode() != swapped.Mode() || before.Size() != swapped.Size() || !before.ModTime().Equal(swapped.ModTime()) {
		t.Fatal("fixture is not a different inode with identical metadata", err)
	}
	hooks := replaceHooks{close: func(stage *os.File) error {
		if err := stage.Close(); err != nil {
			return err
		}
		return os.Rename(exchange, path)
	}}
	result, err := replaceAt(context.Background(), binary, "v1.2.3", directory, "jev", hooks)
	after, statErr := os.Stat(path)
	data, readErr := os.ReadFile(path)
	entries, dirErr := os.ReadDir(directory)
	preserved := statErr == nil && os.SameFile(swapped, after) && swapped.Mode() == after.Mode() &&
		swapped.Size() == after.Size() && swapped.ModTime().Equal(after.ModTime())
	if err == nil || result.Installed || !preserved || readErr != nil || dirErr != nil || !bytes.Equal(data, original) || len(entries) != 1 {
		t.Fatal("inode exchange was overwritten or task files leaked", result, err)
	}
}

func TestLatestDownloadReplaceIntegration(t *testing.T) {
	original, candidate := replacementFixture(t)
	name := "jevwise_" + candidate.tag + "_" + runtime.GOOS + "_" + runtime.GOARCH
	for _, malformed := range []bool{false, true} {
		t.Run(fmt.Sprintf("malformed=%v", malformed), func(t *testing.T) {
			directory, path := replacementTarget(t, original)
			before, _ := os.Stat(path)
			manifest := fmt.Sprintf("%x  %s\n", candidate.digest, name)
			if malformed {
				manifest = strings.Repeat("z", 64) + "  " + name + "\n"
			}
			metadata := fmt.Sprintf(`{"tag_name":%q,"draft":false,"prerelease":false,"assets":[{"name":%q,"size":%d},{"name":"SHA256SUMS","size":%d}]}`, candidate.tag, name, len(candidate.data), len(manifest))
			base := "https://github.com/bitbrew-dev/jevwise/releases/download/" + candidate.tag + "/"
			expected := []string{latestURL, base + "SHA256SUMS", base + name}
			var bodies []*trackedBody
			client := NewClient(&http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
				index := len(bodies)
				if index >= len(expected) || r.URL.String() != expected[index] || r.Header.Get("User-Agent") != "jevwise-update" {
					t.Fatalf("unexpected integration request %s", r.URL)
				}
				var reader io.Reader = bytes.NewReader(candidate.data)
				if index == 0 {
					reader = strings.NewReader(metadata)
				} else if index == 1 {
					reader = strings.NewReader(manifest)
				}
				response := assetResponse(http.StatusOK, reader, nil)
				bodies = append(bodies, response.Body.(*trackedBody))
				return response, nil
			})})
			ctx := context.Background()
			release, err := client.Latest(ctx)
			if err != nil {
				t.Fatal(err)
			}
			binary, err := client.Download(ctx, release, runtime.GOOS, runtime.GOARCH)
			result := InstallResult{}
			if err == nil {
				result, err = replaceAt(ctx, binary, "v1.2.3", directory, "jev", replaceHooks{})
			}
			after, _ := os.Stat(path)
			data, _ := os.ReadFile(path)
			entries, _ := os.ReadDir(directory)
			if malformed {
				if err == nil || binary != nil || result.Installed || len(bodies) != 2 || !os.SameFile(before, after) || !bytes.Equal(data, original) {
					t.Fatal("malformed checksum installed or changed target", result, err)
				}
			} else if err != nil || !result.Installed || len(bodies) != 3 || os.SameFile(before, after) || !bytes.Equal(data, candidate.data) || after.Mode().Perm() != before.Mode().Perm() {
				t.Fatal("verified integration installation failed", result, err)
			}
			if len(entries) != 1 {
				t.Fatal("integration left task files behind")
			}
			for _, body := range bodies {
				if !body.closed {
					t.Fatal("integration response body leaked")
				}
			}
		})
	}
}
