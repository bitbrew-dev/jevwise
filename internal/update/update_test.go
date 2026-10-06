package update

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/cookiejar"
	"strings"
	"testing"
	"time"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

type trackedBody struct {
	io.Reader
	closed bool
}

func (b *trackedBody) Close() error { b.closed = true; return nil }

type readFunc func([]byte) (int, error)

func (f readFunc) Read(p []byte) (int, error) { return f(p) }

const stable = `{"tag_name":"v1.2.3","draft":false,"prerelease":false,"assets":[]}`

func TestCompare(t *testing.T) {
	for _, tc := range []struct {
		a, b string
		want int
	}{
		{"v0.0.0", "v0.0.0", 0}, {"v1.9.9", "v2.0.0", -1},
		{"v1.10.0", "v1.9.9", 1}, {"v1.2.3", "v1.2.4", -1},
		{"v18446744073709551615.0.0", "v0.0.0", 1},
	} {
		got, err := Compare(tc.a, tc.b)
		if err != nil || got != tc.want {
			t.Fatalf("Compare(%q,%q)=%d,%v", tc.a, tc.b, got, err)
		}
	}
	for _, version := range []string{"", "dev", "1.2.3", "v1.2", "v1.2.3.4", "v01.2.3", "v1.02.3", "v1.2.03", "v+1.2.3", "v1.-2.3", "v1..3", "v1.2.3-rc1", "v1.2.3+build", "v１.2.3", "v1.2.3\n", "v18446744073709551616.0.0"} {
		if _, err := Compare(version, "v1.2.3"); err == nil {
			t.Fatalf("accepted %q", version)
		}
		if _, err := Compare("v1.2.3", version); err == nil {
			t.Fatalf("accepted latest %q", version)
		}
	}
}

func TestLatestClientIsolation(t *testing.T) {
	jar, _ := cookiejar.New(nil)
	caller := &http.Client{Jar: jar, CheckRedirect: func(*http.Request, []*http.Request) error { t.Fatal("caller redirect hook used"); return nil }}
	calls := 0
	var body *trackedBody
	caller.Transport = roundTripFunc(func(r *http.Request) (*http.Response, error) {
		calls++
		if r.URL.String() != latestURL || r.Method != "GET" || r.Header.Get("Accept") != "application/vnd.github+json" || r.Header.Get("X-GitHub-Api-Version") != "2026-03-10" || r.Header.Get("User-Agent") == "" {
			t.Fatalf("incorrect public request: %v", r)
		}
		if r.Header.Get("Authorization") != "" || r.Header.Get("Cookie") != "" {
			t.Fatal("credentials sent")
		}
		body = &trackedBody{Reader: strings.NewReader(stable)}
		return &http.Response{StatusCode: 200, Body: body, Header: make(http.Header)}, nil
	})
	client := NewClient(caller)
	req, _ := http.NewRequest("GET", latestURL, nil)
	jar.SetCookies(req.URL, []*http.Cookie{{Name: "secret", Value: "private"}})
	caller.Transport = roundTripFunc(func(*http.Request) (*http.Response, error) { t.Fatal("client was not copied"); return nil, nil })
	got, err := client.Latest(context.Background())
	if err != nil || got.Tag != "v1.2.3" || got.Assets == nil || !body.closed || calls != 1 {
		t.Fatalf("Latest=%+v,%v closed=%v", got, err, body.closed)
	}
	if caller.Jar != jar || caller.CheckRedirect == nil || client.httpClient.Jar != nil {
		t.Fatal("caller settings changed")
	}
	defaults := NewClient(nil)
	transport, ok := defaults.httpClient.Transport.(*http.Transport)
	if !ok || defaults.httpClient.Timeout != 0 || transport.IdleConnTimeout != 90*time.Second || transport.TLSHandshakeTimeout != 10*time.Second || transport.MaxIdleConns != 10 || transport.DialContext == nil || transport.Proxy == nil || !transport.ForceAttemptHTTP2 {
		t.Fatal("missing context-controlled dedicated defaults")
	}
	client.httpClient.Transport = roundTripFunc(func(r *http.Request) (*http.Response, error) {
		calls++
		body = &trackedBody{Reader: strings.NewReader("private response")}
		return &http.Response{StatusCode: 302, Body: body, Header: http.Header{"Location": {"https://private.invalid/secret"}}}, nil
	})
	if _, err := client.Latest(context.Background()); err == nil || calls != 2 || !body.closed || strings.Contains(err.Error(), "private") {
		t.Fatalf("redirect: %v calls=%d", err, calls)
	}
}

func TestLatestMetadata(t *testing.T) {
	for _, tc := range []struct {
		name, body string
		status     int
		good       bool
	}{
		{"stable", stable, 200, true},
		{"exact-limit", stable + strings.Repeat(" ", metadataLimit-len(stable)), 200, true},
		{"assets", strings.Replace(stable, "[]", `[{"name":"jev_v1.2.3_linux_amd64","size":12}]`, 1), 200, true},
		{"missing-assets", strings.Replace(stable, `,"assets":[]`, "", 1), 200, true},
		{"null-assets", strings.Replace(stable, "[]", "null", 1), 200, true},
		{"not-found", "private", 404, false}, {"forbidden", "private", 403, false}, {"server", "private", 500, false},
		{"invalid-json", "private", 200, false}, {"trailing", stable + stable, 200, false}, {"large", strings.Repeat(" ", metadataLimit+1), 200, false},
		{"missing-tag", strings.Replace(stable, `"tag_name":"v1.2.3",`, "", 1), 200, false},
		{"null-tag", strings.Replace(stable, `"v1.2.3"`, "null", 1), 200, false},
		{"tag", strings.Replace(stable, "v1.2.3", "dev", 1), 200, false},
		{"draft", strings.Replace(stable, `"draft":false`, `"draft":true`, 1), 200, false},
		{"prerelease", strings.Replace(stable, `"prerelease":false`, `"prerelease":true`, 1), 200, false},
		{"missing-draft", strings.Replace(stable, `"draft":false,`, "", 1), 200, false},
		{"missing-prerelease", strings.Replace(stable, `"prerelease":false,`, "", 1), 200, false},
		{"null-draft", strings.Replace(stable, `"draft":false`, `"draft":null`, 1), 200, false},
		{"null-flag", strings.Replace(stable, `"prerelease":false`, `"prerelease":null`, 1), 200, false},
		{"wrong-flag", strings.Replace(stable, `"draft":false`, `"draft":"private"`, 1), 200, false},
		{"wrong-assets", strings.Replace(stable, "[]", "{}", 1), 200, false},
		{"missing-name", strings.Replace(stable, "[]", `[{"size":1}]`, 1), 200, false},
		{"missing-size", strings.Replace(stable, "[]", `[{"name":"private"}]`, 1), 200, false},
		{"null-name", strings.Replace(stable, "[]", `[{"name":null,"size":1}]`, 1), 200, false},
		{"null-size", strings.Replace(stable, "[]", `[{"name":"private","size":null}]`, 1), 200, false},
		{"overflow-size", strings.Replace(stable, "[]", `[{"name":"private","size":9223372036854775808}]`, 1), 200, false},
		{"negative-size", strings.Replace(stable, "[]", `[{"name":"private","size":-1}]`, 1), 200, false},
		{"wrong-size", strings.Replace(stable, "[]", `[{"name":"private","size":"private"}]`, 1), 200, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			body := &trackedBody{Reader: strings.NewReader(tc.body)}
			client := NewClient(&http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
				return &http.Response{StatusCode: tc.status, Body: body}, nil
			})})
			got, err := client.Latest(context.Background())
			if (err == nil) != tc.good || !body.closed {
				t.Fatalf("Latest=%+v,%v closed=%v", got, err, body.closed)
			}
			if tc.status == 404 && !errors.Is(err, ErrNoRelease) {
				t.Fatalf("not404 sentinel: %v", err)
			}
			if err != nil && strings.Contains(err.Error(), "private") {
				t.Fatalf("unsafe error: %v", err)
			}
			if tc.name == "invalid-json" {
				var syntax *json.SyntaxError
				if !errors.As(err, &syntax) {
					t.Fatalf("decode cause lost: %v", err)
				}
			}
			if tc.name == "assets" && (len(got.Assets) != 1 || got.Assets[0].Size != 12) {
				t.Fatalf("assets=%v", got.Assets)
			}
		})
	}
}

func TestLatestErrorsAndCancellation(t *testing.T) {
	secret := errors.New("https://private.invalid/token private cause")
	for _, transportFailure := range []bool{true, false} {
		body := &trackedBody{Reader: readFunc(func([]byte) (int, error) { return 0, secret })}
		client := NewClient(&http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
			if transportFailure {
				return nil, secret
			}
			return &http.Response{StatusCode: 200, Body: body}, nil
		})})
		_, err := client.Latest(context.Background())
		if !errors.Is(err, secret) || strings.Contains(err.Error(), "private") || (!transportFailure && !body.closed) {
			t.Fatalf("error=%v closed=%v", err, body.closed)
		}
	}
	for _, cancelAt := range []string{"before", "deadline", "transport", "read"} {
		t.Run(cancelAt, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			want := error(context.Canceled)
			if cancelAt == "deadline" {
				ctx, cancel = context.WithDeadline(context.Background(), time.Unix(0, 0))
				defer cancel()
				want = context.DeadlineExceeded
			}
			calls := 0
			body := &trackedBody{Reader: readFunc(func(p []byte) (int, error) { cancel(); return copy(p, stable), io.EOF })}
			client := NewClient(&http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
				calls++
				if cancelAt == "transport" {
					cancel()
				}
				return &http.Response{StatusCode: 200, Body: body}, nil
			})})
			if cancelAt == "before" {
				cancel()
			}
			_, err := client.Latest(ctx)
			if !errors.Is(err, want) || ((cancelAt == "before" || cancelAt == "deadline") && calls != 0) || (calls > 0 && !body.closed) {
				t.Fatalf("cancel=%v calls=%d closed=%v", err, calls, body.closed)
			}
		})
	}
	if _, err := NewClient(nil).Latest(nil); err == nil {
		t.Fatal("nil context accepted")
	}
	if _, err := (*Client)(nil).Latest(context.Background()); err == nil {
		t.Fatal("nil client accepted")
	}
}
