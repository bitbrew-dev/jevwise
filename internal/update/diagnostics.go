package update

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"net"
	"net/http"
	"strconv"

	"github.com/bitbrew-dev/jevwise/internal/debuglog"
)

var errRedirectRefused = errors.New("release redirect refused")

// LookupMessage exposes only locally assigned categories and numeric HTTP status,
// never an upstream body, header text, URL error or underlying error message.
func LookupMessage(err error) string {
	var failure *safeError
	if errors.As(err, &failure) {
		if failure.reason == "rate_limited" {
			return "GitHub release check is rate limited; wait for the limit to reset and retry (--debug shows rate-limit details)"
		}
		if failure.status != 0 && failure.status != http.StatusOK {
			return fmt.Sprintf("cannot check Jevwise releases (HTTP %d); rerun with --debug", failure.status)
		}
	}
	return "cannot check Jevwise releases; rerun with --debug"
}

// Classify typed errors only. Error strings may contain proxy credentials or URLs.
func networkReason(err error) string {
	var dns *net.DNSError
	var certificate *tls.CertificateVerificationError
	var network net.Error
	switch {
	case errors.Is(err, context.Canceled):
		return "canceled"
	case errors.Is(err, context.DeadlineExceeded):
		return "timeout"
	case errors.Is(err, errRedirectRefused):
		return "redirect_refused"
	case errors.As(err, &dns):
		return "dns"
	case errors.As(err, &certificate):
		return "tls_certificate"
	case errors.As(err, &network) && network.Timeout():
		return "timeout"
	default:
		return "network"
	}
}

// HTTP diagnostics use fixed stage names and bounded numeric metadata only.
// A 403 alone is not evidence of rate limiting; a 429 is.
func logHTTP(ctx context.Context, stage string, res *http.Response) bool {
	debuglog.Count(ctx, stage+".http_status", res.StatusCode)
	number := func(header, event string) int64 {
		value := res.Header.Get(header)
		if value == "" || len(value) > 19 {
			return -1
		}
		for _, b := range value {
			if b < '0' || b > '9' {
				return -1
			}
		}
		n, err := strconv.ParseInt(value, 10, strconv.IntSize)
		if err != nil {
			return -1
		}
		debuglog.Count(ctx, stage+event, int(n))
		return n
	}
	remaining := number("X-RateLimit-Remaining", ".rate_limit.remaining")
	number("X-RateLimit-Reset", ".rate_limit.reset_unix")
	retry := number("Retry-After", ".rate_limit.retry_after_seconds")
	return res.StatusCode == 429 || res.StatusCode == 403 && (remaining == 0 || retry >= 0)
}
