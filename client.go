// Package typesafe provides a context-aware client for the Typesafe API.
package typesafe

import (
	"fmt"
	"net"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"
)

const (
	DefaultBaseURL = "https://api.typesafe.ai"
	DefaultModel   = "jev-latest"
	DefaultTimeout = 10 * time.Second
)

// ClientOptions configures a Client. Empty strings fall back to TYPESAFE_API_KEY,
// TYPESAFE_BASE_URL and TYPESAFE_DEFAULT_MODEL, respectively, then SDK defaults.
// Timeout zero selects DefaultTimeout; negative values are invalid. The timeout
// is the SDK operation budget, independent of an injected HTTP client's timeout.
// Headers are copied. HTTPClient remains caller-owned and must not be mutated
// concurrently with SDK operations; Close never closes its idle connections.
type ClientOptions struct {
	APIKey       string
	BaseURL      string
	DefaultModel string
	Timeout      time.Duration
	Headers      http.Header
	HTTPClient   *http.Client
}

// Client holds immutable configuration and a reusable HTTP connection pool.
// Construct it with NewClient rather than using its zero value.
type Client struct {
	apiKey         string
	baseURL        string
	defaultModel   string
	timeout        time.Duration
	headers        http.Header
	httpClient     *http.Client
	ownedTransport *http.Transport
}

// NewClient resolves explicit options before environment values and defaults.
// Validation errors never contain the supplied API key or URL.
func NewClient(options ClientOptions) (*Client, error) {
	key := strings.TrimSpace(resolveOption(options.APIKey, "TYPESAFE_API_KEY", ""))
	if key == "" {
		return nil, fmt.Errorf("typesafe: API key is required")
	}
	for _, b := range []byte(key) {
		if b < 0x21 || b > 0x7e {
			return nil, fmt.Errorf("typesafe: API key must contain printable ASCII without whitespace")
		}
	}
	base := strings.TrimSpace(resolveOption(options.BaseURL, "TYPESAFE_BASE_URL", DefaultBaseURL))
	u, err := url.Parse(base)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Hostname() == "" ||
		u.User != nil || u.RawQuery != "" || u.ForceQuery || strings.Contains(base, "#") {
		return nil, fmt.Errorf("typesafe: base URL must be absolute HTTP(S) without credentials, query or fragment")
	}
	model := strings.TrimSpace(resolveOption(options.DefaultModel, "TYPESAFE_DEFAULT_MODEL", DefaultModel))
	if model == "" {
		return nil, fmt.Errorf("typesafe: default model must not be blank")
	}
	timeout := options.Timeout
	if timeout < 0 {
		return nil, fmt.Errorf("typesafe: timeout must be positive")
	}
	if timeout == 0 {
		timeout = DefaultTimeout
	}
	c := &Client{apiKey: key, baseURL: strings.TrimRight(base, "/"), defaultModel: model,
		timeout: timeout, headers: options.Headers.Clone(), httpClient: options.HTTPClient}
	if c.headers == nil {
		c.headers = make(http.Header)
	}
	if c.httpClient == nil {
		// Never share the process-wide default connection pool. If applications
		// replaced DefaultTransport, retain standard defaults in our own pool.
		if standard, ok := http.DefaultTransport.(*http.Transport); ok {
			c.ownedTransport = standard.Clone()
		} else {
			c.ownedTransport = &http.Transport{Proxy: http.ProxyFromEnvironment,
				DialContext:       (&net.Dialer{Timeout: 30 * time.Second, KeepAlive: 30 * time.Second}).DialContext,
				ForceAttemptHTTP2: true, MaxIdleConns: 100, IdleConnTimeout: 90 * time.Second,
				TLSHandshakeTimeout: 10 * time.Second, ExpectContinueTimeout: time.Second}
		}
		c.httpClient = &http.Client{Transport: c.ownedTransport}
	}
	return c, nil
}

func resolveOption(value, environment, fallback string) string {
	if value != "" {
		return value
	}
	if value = strings.TrimSpace(os.Getenv(environment)); value != "" {
		return value
	}
	return fallback
}

// Close releases SDK-owned idle connections without interrupting active calls.
// It is safe to call repeatedly. Injected HTTP clients remain caller-owned.
func (c *Client) Close() error {
	if c != nil && c.ownedTransport != nil {
		c.ownedTransport.CloseIdleConnections()
	}
	return nil
}
