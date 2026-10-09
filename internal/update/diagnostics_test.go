package update

import (
	"bytes"
	"context"
	"crypto/tls"
	"errors"
	"io"
	"net"
	"net/http"
	"strings"
	"testing"

	"github.com/bitbrew-dev/jevwise/internal/debuglog"
)

func TestLookupDiagnosticsExplainFailuresWithoutLeaks(t *testing.T) {
	for _, test := range []struct {
		name, reason string
		status       int
		headers      http.Header
		body         string
		err          error
	}{
		{"rate", "rate_limited", 403, http.Header{"X-Ratelimit-Remaining": {"0"}, "X-Ratelimit-Reset": {"1791544363"}}, "private-body", nil},
		{"secondary", "rate_limited", 403, http.Header{"Retry-After": {"12"}}, "private-body", nil},
		{"429", "rate_limited", 429, nil, "private-body", nil},
		{"forbidden", "http_status", 403, nil, "private-body", nil},
		{"malicious headers", "http_status", 403, http.Header{"X-Ratelimit-Remaining": {"private-header"}, "X-Ratelimit-Reset": {"9223372036854775808"}, "Retry-After": {"-1"}}, "private-body", nil},
		{"json", "metadata_json", 200, nil, "private-body", nil},
		{"tag", "release_tag", 200, nil, strings.Replace(stable, "v1.2.3", "private-tag", 1), nil},
		{"dns", "dns", 0, nil, "", &net.DNSError{Err: "private-cause", Name: "private-host"}},
		{"tls", "tls_certificate", 0, nil, "", &tls.CertificateVerificationError{Err: errors.New("private-certificate")}},
		{"deadline", "timeout", 0, nil, "", context.DeadlineExceeded},
		{"network", "network", 0, nil, "", errors.New("https://user:private-password@proxy.invalid")},
	} {
		t.Run(test.name, func(t *testing.T) {
			var output bytes.Buffer
			client := NewClient(&http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
				if test.err != nil {
					return nil, test.err
				}
				return &http.Response{StatusCode: test.status, Header: test.headers, Body: io.NopCloser(strings.NewReader(test.body))}, nil
			})})
			_, err := client.Latest(debuglog.WithWriter(context.Background(), &output))
			if err == nil || !strings.Contains(output.String(), "update.lookup.failed."+test.reason) || !strings.Contains(output.String(), latestURL) {
				t.Fatal("failure diagnostics missing")
			}
			if strings.Contains(output.String()+err.Error()+LookupMessage(err), "private") {
				t.Fatal("diagnostics exposed raw data")
			}
			if strings.Contains(LookupMessage(err), "rate limited") != (test.reason == "rate_limited") {
				t.Fatal("HTTP failure incorrectly classified")
			}
			if test.name == "rate" && (!strings.Contains(output.String(), `"value":0`) || !strings.Contains(output.String(), `"value":1791544363`)) {
				t.Fatal("rate-limit numbers missing")
			}
			if test.err != nil && !errors.Is(err, test.err) {
				t.Fatal("underlying cause changed")
			}
		})
	}
}

func TestLookupDiagnosticsAreOptInAnd404Ambiguous(t *testing.T) {
	var output bytes.Buffer
	client := NewClient(&http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: 404, Body: io.NopCloser(strings.NewReader("private-body"))}, nil
	})})
	_, err := client.Latest(debuglog.WithWriter(context.Background(), &output))
	if !errors.Is(err, ErrNoRelease) || !strings.Contains(output.String(), "no_public_release_or_repository") || strings.Contains(output.String(), "private-body") {
		t.Fatal("404 diagnosis changed or leaked")
	}
	before := output.Len()
	client.Latest(context.Background())
	if output.Len() != before {
		t.Fatal("default lookup inherited debug output")
	}
}

func TestAssetDiagnosticsDoNotExposeRedirectsOrBodies(t *testing.T) {
	for _, kind := range []string{"rate", "redirect"} {
		var output bytes.Buffer
		calls := 0
		client := NewClient(&http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
			calls++
			status, headers := 403, http.Header{"X-Ratelimit-Remaining": {"0"}}
			if kind == "redirect" {
				status, headers = 302, http.Header{"Location": {"https://private.invalid/?token=private-secret"}}
			}
			return &http.Response{StatusCode: status, Header: headers, Body: io.NopCloser(strings.NewReader("private-body"))}, nil
		})})
		_, err := client.readAsset(debuglog.WithWriter(context.Background(), &output), "https://github.com/bitbrew-dev/jevwise/releases/download/v1.2.3/SHA256SUMS", 1)
		want := "rate_limited"
		if kind == "redirect" {
			want = "redirect_refused"
		}
		if err == nil || calls != 1 || !strings.Contains(output.String(), "update.asset.failed."+want) || strings.Contains(output.String(), "private") {
			t.Fatal("asset diagnostics missing or exposed remote data")
		}
		if (kind == "redirect") != errors.Is(err, errRedirectRefused) {
			t.Fatal("redirect guard cause changed")
		}
	}
}
