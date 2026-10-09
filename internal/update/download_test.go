package update

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"testing"
)

func downloadFixture(goos, arch string) (Release, map[string]string) {
	name := "jevwise_v1.2.3_" + goos + "_" + arch
	if goos == "windows" {
		name += ".exe"
	}
	payload := "verified binary"
	manifest := fmt.Sprintf("%x  %s\n", sha256.Sum256([]byte(payload)), name)
	return Release{Tag: "v1.2.3", Assets: []Asset{{Name: name, Size: int64(len(payload))}, {Name: "SHA256SUMS", Size: int64(len(manifest))}}}, map[string]string{name: payload, "SHA256SUMS": manifest}
}

func assetResponse(status int, body io.Reader, headers http.Header) *http.Response {
	return &http.Response{StatusCode: status, Body: &trackedBody{Reader: body}, Header: headers}
}

func TestDownloadPlatforms(t *testing.T) {
	for _, os := range []string{"linux", "darwin", "windows"} {
		for _, arch := range []string{"amd64", "arm64"} {
			release, content := downloadFixture(os, arch)
			var bodies []*trackedBody
			var names []string
			caller := &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error { t.Fatal("caller redirect policy used"); return nil }}
			caller.Transport = roundTripFunc(func(r *http.Request) (*http.Response, error) {
				name := r.URL.Path[strings.LastIndex(r.URL.Path, "/")+1:]
				if r.URL.String() != "https://github.com/bitbrew-dev/jevwise/releases/download/v1.2.3/"+name || content[name] == "" || r.Method != "GET" || r.Header.Get("User-Agent") != "jevwise-update" {
					t.Fatalf("wrong asset request: %v", r)
				}
				if r.Header.Get("Authorization") != "" || r.Header.Get("Cookie") != "" {
					t.Fatal("credentials sent")
				}
				names = append(names, name)
				res := assetResponse(200, strings.NewReader(content[name]), nil)
				bodies = append(bodies, res.Body.(*trackedBody))
				return res, nil
			})
			client := NewClient(caller)
			caller.Transport = nil
			binary, err := client.Download(context.Background(), release, os, arch)
			if err != nil || string(binary.data) != "verified binary" || binary.tag != release.Tag || binary.goos != os || binary.goarch != arch || binary.digest != sha256.Sum256(binary.data) {
				t.Fatalf("binary=%+v,%v", binary, err)
			}
			if len(names) != 2 || names[0] != "SHA256SUMS" || names[1] != release.Assets[0].Name {
				t.Fatalf("request order=%v", names)
			}
			for _, body := range bodies {
				if !body.closed {
					t.Fatal("body not closed")
				}
			}
		}
	}
}

func TestDownloadPreflight(t *testing.T) {
	release, _ := downloadFixture("linux", "amd64")
	for _, tc := range []struct {
		name     string
		change   func(*Release)
		os, arch string
	}{
		{"tag", func(r *Release) { r.Tag = "v01.2.3" }, "linux", "amd64"},
		{"missing", func(r *Release) { r.Assets = r.Assets[:1] }, "linux", "amd64"},
		{"wrong-prefix", func(r *Release) { r.Assets[0].Name = strings.Replace(r.Assets[0].Name, "jevwise_", "other_", 1) }, "linux", "amd64"},
		{"wrong-platform", func(r *Release) { r.Assets[0].Name = strings.Replace(r.Assets[0].Name, "_linux_", "_darwin_", 1) }, "linux", "amd64"},
		{"duplicate", func(r *Release) { r.Assets = append(r.Assets, r.Assets[0]) }, "linux", "amd64"},
		{"duplicate-manifest", func(r *Release) { r.Assets = append(r.Assets, r.Assets[1]) }, "linux", "amd64"},
		{"zero", func(r *Release) { r.Assets[0].Size = 0 }, "linux", "amd64"},
		{"negative", func(r *Release) { r.Assets[1].Size = -1 }, "linux", "amd64"},
		{"binary-limit", func(r *Release) { r.Assets[0].Size = binaryLimit + 1 }, "linux", "amd64"},
		{"manifest-limit", func(r *Release) { r.Assets[1].Size = manifestLimit + 1 }, "linux", "amd64"},
		{"os", func(*Release) {}, "freebsd", "amd64"}, {"arch", func(*Release) {}, "linux", "386"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := release
			r.Assets = append([]Asset(nil), release.Assets...)
			tc.change(&r)
			client := NewClient(&http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) { t.Fatal("invalid metadata dispatched"); return nil, nil })})
			if binary, err := client.Download(context.Background(), r, tc.os, tc.arch); binary != nil || err == nil {
				t.Fatalf("result=%+v,%v", binary, err)
			}
		})
	}
}

func TestManifest(t *testing.T) {
	release, content := downloadFixture("linux", "amd64")
	name := release.Assets[0].Name
	good := strings.TrimSuffix(content["SHA256SUMS"], "\n")
	for _, text := range []string{good, good + "\n", good + "\n" + strings.Replace(good, name, "other-valid.asset", 1)} {
		if _, err := manifestDigest([]byte(text), name); err != nil {
			t.Fatalf("valid manifest: %v", err)
		}
	}
	for _, text := range []string{"", good + "\n\n", good + "\r\n", strings.ToUpper(good), good[1:], strings.Replace(good, "  ", " ", 1), good + "\n" + good, strings.Replace(good, name, "other", 1), strings.Replace(good, name, "../private", 1), strings.Replace(good, name, ".", 1), strings.Replace(good, name, "..", 1), strings.Replace(good, name, "bad name", 1), strings.Replace(good, name, "ß", 1), "z" + good[1:]} {
		if _, err := manifestDigest([]byte(text), name); err == nil {
			t.Fatalf("accepted malformed manifest %q", text)
		}
	}
}

func TestDownloadReadFailures(t *testing.T) {
	for _, tc := range []struct {
		name              string
		status            int
		manifest, payload string
		readErr           bool
	}{
		{"checksum", 200, "", "corrupt binary!", false}, {"short", 200, "", "x", false}, {"long", 200, "", strings.Repeat("x", 100), false},
		{"manifest-size", 200, "x", "", false}, {"manifest-invalid", 200, strings.Repeat("x", 97), "", false},
		{"status", 403, "", "", false}, {"read", 200, "", "", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			release, content := downloadFixture("linux", "amd64")
			if tc.manifest != "" {
				content["SHA256SUMS"] = tc.manifest
				if tc.name == "manifest-invalid" {
					release.Assets[1].Size = int64(len(tc.manifest))
				}
			}
			if tc.payload != "" {
				content[release.Assets[0].Name] = tc.payload
			}
			secret := errors.New("private signed URL cause")
			read := 0
			var bodies []*trackedBody
			client := NewClient(&http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
				name := r.URL.Path[strings.LastIndex(r.URL.Path, "/")+1:]
				reader := strings.NewReader(content[name])
				res := assetResponse(tc.status, readFunc(func(p []byte) (int, error) {
					if tc.readErr {
						return 0, secret
					}
					n, err := reader.Read(p)
					read += n
					return n, err
				}), nil)
				bodies = append(bodies, res.Body.(*trackedBody))
				return res, nil
			})})
			binary, err := client.Download(context.Background(), release, "linux", "amd64")
			if binary != nil || err == nil || strings.Contains(err.Error(), "private") || (tc.readErr && !errors.Is(err, secret)) {
				t.Fatalf("result=%+v,%v", binary, err)
			}
			if tc.name == "long" && read != len(content["SHA256SUMS"])+int(release.Assets[0].Size)+1 {
				t.Fatalf("unbounded read=%d", read)
			}
			for _, body := range bodies {
				if !body.closed {
					t.Fatal("failed body not closed")
				}
			}
		})
	}
}

func TestAssetRedirects(t *testing.T) {
	original := "https://github.com/bitbrew-dev/jevwise/releases/download/v1.2.3/SHA256SUMS"
	for _, address := range []string{original, "https://release-assets.githubusercontent.com/asset?signature=private", "https://objects.githubusercontent.com/asset?signature=private"} {
		u, _ := url.Parse(address)
		if !assetURLAllowed(u, original) {
			t.Fatalf("allowed URL refused: %s", address)
		}
	}
	for _, address := range []string{"http://objects.githubusercontent.com/asset", "https://evil.invalid/asset", "https://github.com/other/repo/asset", original + "?private=1", original + "#fragment", "https://user:pass@objects.githubusercontent.com/asset", "https://objects.githubusercontent.com:443/asset", "https://github.com:443/asset", "https://github.com.evil.invalid/asset", "https:opaque"} {
		t.Run(address, func(t *testing.T) {
			calls := 0
			res := assetResponse(302, strings.NewReader("private"), http.Header{"Location": {address}})
			client := NewClient(&http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) { calls++; return res, nil })})
			release, _ := downloadFixture("linux", "amd64")
			if _, err := client.Download(context.Background(), release, "linux", "amd64"); err == nil || calls != 1 || !res.Body.(*trackedBody).closed || strings.Contains(err.Error(), "private") {
				t.Fatalf("redirect=%v calls=%d", err, calls)
			}
		})
	}
	for _, loop := range []bool{false, true} {
		release, content := downloadFixture("linux", "amd64")
		calls := 0
		var bodies []*trackedBody
		client := NewClient(&http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
			calls++
			if r.Header.Get("Referer") != "" || r.Header.Get("Cookie") != "" || r.Header.Get("Authorization") != "" {
				t.Fatal("redirect leaked credentials or Referer")
			}
			res := assetResponse(302, strings.NewReader("private"), http.Header{"Location": {"https://release-assets.githubusercontent.com/asset?signature=private"}})
			if !loop && r.URL.Host == "release-assets.githubusercontent.com" {
				res.Header.Set("Location", "https://objects.githubusercontent.com/asset?signature=private")
			}
			if !loop && r.URL.Host == "objects.githubusercontent.com" {
				name := "SHA256SUMS"
				if calls > 3 {
					name = release.Assets[0].Name
				}
				res = assetResponse(200, strings.NewReader(content[name]), nil)
			}
			bodies = append(bodies, res.Body.(*trackedBody))
			return res, nil
		})})
		_, err := client.Download(context.Background(), release, "linux", "amd64")
		if (err != nil) != loop || calls != 6 {
			t.Fatalf("loop=%v err=%v calls=%d", loop, err, calls)
		}
		for _, body := range bodies {
			if !body.closed {
				t.Fatal("redirect body not closed")
			}
		}
	}
}

func TestDownloadCancellationAndTransport(t *testing.T) {
	release, _ := downloadFixture("linux", "amd64")
	for _, phase := range []string{"before", "transport", "read", "failure"} {
		ctx, cancel := context.WithCancel(context.Background())
		secret := errors.New("private transport cause")
		calls := 0
		body := &trackedBody{Reader: readFunc(func([]byte) (int, error) { cancel(); return 0, secret })}
		client := NewClient(&http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
			calls++
			if phase == "failure" {
				return nil, secret
			}
			if phase == "transport" {
				cancel()
			}
			return &http.Response{StatusCode: 200, Body: body}, nil
		})})
		if phase == "before" {
			cancel()
		}
		_, err := client.Download(ctx, release, "linux", "amd64")
		cancel()
		want := error(context.Canceled)
		if phase == "failure" {
			want = secret
		}
		if !errors.Is(err, want) || strings.Contains(err.Error(), "private") || (phase == "before" && calls != 0) || (phase != "before" && phase != "failure" && !body.closed) {
			t.Fatalf("phase=%s err=%v calls=%d", phase, err, calls)
		}
	}
	if _, err := NewClient(nil).Download(nil, release, "linux", "amd64"); err == nil {
		t.Fatal("nil context accepted")
	}
	if _, err := (*Client)(nil).Download(context.Background(), release, "linux", "amd64"); err == nil {
		t.Fatal("nil client accepted")
	}
}

func TestDownloadAssetNameCompatibilityAndNoDowngrade(t *testing.T) {
	tests := []struct{ mode, os, arch string }{{"primary", "linux", "amd64"}}
	for _, os := range []string{"linux", "darwin", "windows"} {
		for _, arch := range []string{"amd64", "arm64"} {
			tests = append(tests, struct{ mode, os, arch string }{"legacy", os, arch})
		}
	}
	for _, mode := range []string{"zero", "negative", "oversize", "duplicate", "404", "checksum", "manifest", "missing-checksum"} {
		tests = append(tests, struct{ mode, os, arch string }{mode, "linux", "amd64"})
	}
	for _, tc := range tests {
		t.Run(tc.mode+"/"+tc.os+"/"+tc.arch, func(t *testing.T) {
			release, content := downloadFixture(tc.os, tc.arch)
			primary := release.Assets[0].Name
			legacy := strings.Replace(primary, "jevwise_", "jev_", 1)
			content[legacy] = "legacy binary"
			legacyRow := fmt.Sprintf("%x  %s\n", sha256.Sum256([]byte(content[legacy])), legacy)
			content["SHA256SUMS"] += legacyRow
			release.Assets = append(release.Assets, Asset{Name: legacy, Size: int64(len(content[legacy]))})
			preflight := false
			switch tc.mode {
			case "legacy":
				release.Assets = release.Assets[1:]
				content["SHA256SUMS"] = legacyRow
			case "zero", "negative", "oversize", "duplicate":
				preflight = true
				switch tc.mode {
				case "zero":
					release.Assets[0].Size = 0
				case "negative":
					release.Assets[0].Size = -1
				case "oversize":
					release.Assets[0].Size = binaryLimit + 1
				case "duplicate":
					release.Assets = append(release.Assets, release.Assets[0])
				}
			case "checksum":
				content[primary] = "corrupt binary!"
			case "manifest":
				content["SHA256SUMS"] = "z" + content["SHA256SUMS"][1:]
			case "missing-checksum":
				content["SHA256SUMS"] = legacyRow
			}
			for i := range release.Assets {
				if release.Assets[i].Name == "SHA256SUMS" {
					release.Assets[i].Size = int64(len(content["SHA256SUMS"]))
				}
			}
			var names []string
			client := NewClient(&http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
				name := r.URL.Path[strings.LastIndex(r.URL.Path, "/")+1:]
				names = append(names, name)
				if preflight || (name == legacy && tc.mode != "legacy") {
					t.Fatal("invalid primary triggered HTTP or legacy downgrade")
				}
				if content[name] == "" || r.Header.Get("User-Agent") != "jevwise-update" || r.Header.Get("Authorization") != "" || r.Header.Get("Cookie") != "" {
					t.Fatal("unexpected asset request or credentials")
				}
				status := http.StatusOK
				if tc.mode == "404" && name == primary {
					status = http.StatusNotFound
				}
				return assetResponse(status, strings.NewReader(content[name]), nil), nil
			})})
			binary, err := client.Download(context.Background(), release, tc.os, tc.arch)
			if tc.mode != "primary" && tc.mode != "legacy" {
				if err == nil || binary != nil || (preflight && len(names) != 0) {
					t.Fatal("primary failure accepted or retried", err)
				}
				return
			}
			chosen := primary
			if tc.mode == "legacy" {
				chosen = legacy
			}
			if err != nil || binary == nil || string(binary.data) != content[chosen] || len(names) != 2 || names[0] != "SHA256SUMS" || names[1] != chosen {
				t.Fatal("wrong primary/legacy selection", names, err)
			}
		})
	}
}
