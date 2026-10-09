// Package update checks public Jevwise releases without SDK credentials.
package update

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/bitbrew-dev/jevwise/internal/debuglog"
)

const latestURL = "https://api.github.com/repos/bitbrew-dev/jevwise/releases/latest"
const metadataLimit = 1 << 20
const apiVersion = "2026-03-10"

// ErrNoRelease means GitHub has no published stable release.
var ErrNoRelease = errors.New("no published stable Jevwise release")

// Asset describes an artifact without trusting its metadata download URL.
type Asset struct {
	Name string
	Size int64
}

// Release describes stable metadata; assets may not yet have been published.
type Release struct {
	Tag    string
	Assets []Asset
}

type safeError struct {
	cause  error
	reason string
	status int
}

func (e *safeError) Error() string { return "cannot check Jevwise release" }
func (e *safeError) Unwrap() error { return e.cause }

// Client checks one fixed public repository using isolated HTTP settings.
type Client struct{ httpClient *http.Client }

// NewClient copies the supplied client and removes cookies and redirects.
// With nil client, the caller context controls the total deadline.
func NewClient(client *http.Client) *Client {
	if client == nil {
		client = &http.Client{Transport: &http.Transport{
			Proxy: http.ProxyFromEnvironment, ForceAttemptHTTP2: true,
			DialContext:     (&net.Dialer{Timeout: 30 * time.Second}).DialContext,
			IdleConnTimeout: 90 * time.Second, TLSHandshakeTimeout: 10 * time.Second,
			MaxIdleConns: 10,
		}}
	}
	copy := *client
	copy.Jar = nil
	copy.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	return &Client{httpClient: &copy}
}

// Latest obtains bounded metadata for the newest stable release.
func (c *Client) Latest(ctx context.Context) (_ Release, resultErr error) {
	finish := debuglog.Trace(ctx, "update.lookup")
	defer func() { finish(resultErr) }()
	status := 0
	fail := func(reason string, err error) (Release, error) {
		if ctx != nil && ctx.Err() != nil {
			err = ctx.Err()
			reason = networkReason(err)
		}
		debuglog.Event(ctx, "update.lookup.failed."+reason)
		return Release{}, &safeError{cause: err, reason: reason, status: status}
	}
	if ctx == nil || c == nil || c.httpClient == nil {
		return fail("invalid_client", errors.New("invalid release client or context"))
	}
	if err := ctx.Err(); err != nil {
		return fail(networkReason(err), err)
	}
	debuglog.Event(ctx, "update.lookup.GET "+latestURL)
	debuglog.Event(ctx, "update.lookup.api_version."+apiVersion)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, latestURL, nil)
	if err != nil {
		return fail(networkReason(err), err)
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("X-GitHub-Api-Version", apiVersion)
	req.Header.Set("User-Agent", "jevwise-update")
	res, err := c.httpClient.Do(req)
	if err != nil {
		return fail(networkReason(err), err)
	}
	defer res.Body.Close()
	if err := ctx.Err(); err != nil {
		return fail(networkReason(err), err)
	}
	status = res.StatusCode
	rateLimited := logHTTP(ctx, "update.lookup", res)
	if res.StatusCode == http.StatusNotFound {
		debuglog.Event(ctx, "update.lookup.no_public_release_or_repository")
		return Release{}, ErrNoRelease
	}
	if res.StatusCode != http.StatusOK {
		reason := "http_status"
		if rateLimited {
			reason = "rate_limited"
		}
		return fail(reason, errors.New("unexpected release status"))
	}
	body, err := io.ReadAll(io.LimitReader(res.Body, metadataLimit+1))
	if err != nil {
		return fail("response_read", err)
	}
	if len(body) > metadataLimit {
		return fail("metadata_size", errors.New("release metadata too large"))
	}
	var wire struct {
		Tag        string `json:"tag_name"`
		Draft      *bool  `json:"draft"`
		Prerelease *bool  `json:"prerelease"`
		Assets     []struct {
			Name *string `json:"name"`
			Size *int64  `json:"size"`
		} `json:"assets"`
	}
	if err := json.Unmarshal(body, &wire); err != nil {
		return fail("metadata_json", err)
	}
	if _, err := Compare(wire.Tag, wire.Tag); err != nil {
		return fail("release_tag", err)
	}
	if wire.Draft == nil || *wire.Draft || wire.Prerelease == nil || *wire.Prerelease {
		return fail("release_not_stable", errors.New("release is not explicitly stable"))
	}
	release := Release{Tag: wire.Tag, Assets: make([]Asset, 0, len(wire.Assets))}
	for _, asset := range wire.Assets {
		if asset.Name == nil || *asset.Name == "" || asset.Size == nil || *asset.Size < 0 {
			return fail("asset_metadata", errors.New("invalid release asset"))
		}
		release.Assets = append(release.Assets, Asset{Name: *asset.Name, Size: *asset.Size})
	}
	if err := ctx.Err(); err != nil {
		return fail(networkReason(err), err)
	}
	return release, nil
}

// Compare orders canonical vX.Y.Z versions. Development versions are invalid.
func Compare(current, latest string) (int, error) {
	parse := func(version string) ([3]uint64, error) {
		var parts [3]uint64
		if !strings.HasPrefix(version, "v") {
			return parts, errors.New("invalid release version")
		}
		fields := strings.Split(version[1:], ".")
		if len(fields) != 3 {
			return parts, errors.New("invalid release version")
		}
		for i, field := range fields {
			if field == "" || (len(field) > 1 && field[0] == '0') || strings.IndexFunc(field, func(r rune) bool { return r < '0' || r > '9' }) >= 0 {
				return parts, errors.New("invalid release version")
			}
			n, err := strconv.ParseUint(field, 10, 64)
			if err != nil {
				return parts, errors.New("invalid release version")
			}
			parts[i] = n
		}
		return parts, nil
	}
	a, err := parse(current)
	if err != nil {
		return 0, err
	}
	b, err := parse(latest)
	if err != nil {
		return 0, err
	}
	for i := range a {
		if a[i] < b[i] {
			return -1, nil
		}
		if a[i] > b[i] {
			return 1, nil
		}
	}
	return 0, nil
}
