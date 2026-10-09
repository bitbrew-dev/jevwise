package update

import (
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"errors"
	"io"
	"net/http"
	"net/url"
	"strings"
)

const binaryLimit = 64 << 20
const manifestLimit = 64 << 10

// Binary contains verified bytes privately for the package's future installer.
// A checksum provides release integrity, not independent publisher authentication.
type Binary struct {
	data              []byte
	digest            [sha256.Size]byte
	tag, goos, goarch string
}

type downloadError struct{ cause error }

func (e *downloadError) Error() string { return "cannot download Jevwise release" }
func (e *downloadError) Unwrap() error { return e.cause }

// Download verifies one exact platform artifact against the release's SHA256SUMS.
// The caller context controls the total deadline; no files are written.
func (c *Client) Download(ctx context.Context, release Release, goos, goarch string) (*Binary, error) {
	fail := func(err error) (*Binary, error) {
		if ctx != nil && ctx.Err() != nil {
			err = ctx.Err()
		}
		return nil, &downloadError{err}
	}
	if ctx == nil || c == nil || c.httpClient == nil {
		return fail(errors.New("invalid download client or context"))
	}
	if err := ctx.Err(); err != nil {
		return fail(err)
	}
	if _, err := Compare(release.Tag, release.Tag); err != nil {
		return fail(err)
	}
	if (goos != "linux" && goos != "darwin" && goos != "windows") || (goarch != "amd64" && goarch != "arm64") {
		return fail(errors.New("unsupported release platform"))
	}
	name := "jevwise_" + release.Tag + "_" + goos + "_" + goarch
	if goos == "windows" {
		name += ".exe"
	}
	// Metadata presence selects the new name, even when that asset is invalid.
	// Legacy names are only a compatibility path for releases without it.
	primaryPresent := false
	for _, asset := range release.Assets {
		if asset.Name == name {
			primaryPresent = true
			break
		}
	}
	if !primaryPresent {
		name = "jev_" + strings.TrimPrefix(name, "jevwise_")
	}
	sizes := make(map[string]int64, 2)
	for _, asset := range release.Assets {
		limit := int64(binaryLimit)
		if asset.Name == "SHA256SUMS" {
			limit = manifestLimit
		} else if asset.Name != name {
			continue
		}
		if _, exists := sizes[asset.Name]; exists || asset.Size <= 0 || asset.Size > limit {
			return fail(errors.New("invalid or duplicate release asset"))
		}
		sizes[asset.Name] = asset.Size
	}
	if len(sizes) != 2 {
		return fail(errors.New("required release assets missing"))
	}
	base := "https://github.com/bitbrew-dev/jevwise/releases/download/" + release.Tag + "/"
	manifest, err := c.readAsset(ctx, base+"SHA256SUMS", sizes["SHA256SUMS"])
	if err != nil {
		return fail(err)
	}
	digest, err := manifestDigest(manifest, name)
	if err != nil {
		return fail(err)
	}
	data, err := c.readAsset(ctx, base+name, sizes[name])
	if err != nil {
		return fail(err)
	}
	actual := sha256.Sum256(data)
	if subtle.ConstantTimeCompare(actual[:], digest[:]) != 1 {
		return fail(errors.New("release checksum mismatch"))
	}
	if err := ctx.Err(); err != nil {
		return fail(err)
	}
	return &Binary{data: data, digest: digest, tag: release.Tag, goos: goos, goarch: goarch}, nil
}

func (c *Client) readAsset(ctx context.Context, original string, size int64) ([]byte, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	client := *c.httpClient
	client.Jar = nil
	client.CheckRedirect = func(req *http.Request, via []*http.Request) error {
		if len(via) > 5 || !assetURLAllowed(req.URL, original) {
			return errors.New("release redirect refused")
		}
		// Do not forward credentials or leak a signed storage query via Referer.
		req.Header.Del("Authorization")
		req.Header.Del("Cookie")
		req.Header.Del("Referer")
		return ctx.Err()
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, original, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "jevwise-update")
	res, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if res.StatusCode != http.StatusOK {
		return nil, errors.New("unexpected release asset status")
	}
	data, err := io.ReadAll(io.LimitReader(res.Body, size+1))
	if err != nil {
		return nil, err
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if int64(len(data)) != size {
		return nil, errors.New("release asset size mismatch")
	}
	return data, nil
}

func assetURLAllowed(u *url.URL, original string) bool {
	if u.Scheme != "https" || u.User != nil || u.Opaque != "" || u.Fragment != "" {
		return false
	}
	switch u.Host {
	case "github.com":
		return u.String() == original
	case "release-assets.githubusercontent.com", "objects.githubusercontent.com":
		return true
	default:
		return false
	}
}

func manifestDigest(data []byte, chosen string) ([sha256.Size]byte, error) {
	var digest [sha256.Size]byte
	seen := make(map[string]bool)
	found := false
	for _, line := range strings.Split(strings.TrimSuffix(string(data), "\n"), "\n") {
		if len(line) < 67 || line[64:66] != "  " {
			return digest, errors.New("invalid release checksum row")
		}
		hash, name := line[:64], line[66:]
		decoded, err := hex.DecodeString(hash)
		if err != nil || hex.EncodeToString(decoded) != hash || name == "." || name == ".." || strings.IndexFunc(name, func(r rune) bool {
			return !(r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '.' || r == '_' || r == '-')
		}) >= 0 || seen[name] {
			return digest, errors.New("invalid or duplicate release checksum")
		}
		seen[name] = true
		if name == chosen {
			copy(digest[:], decoded)
			found = true
		}
	}
	if !found {
		return digest, errors.New("release checksum missing")
	}
	return digest, nil
}
