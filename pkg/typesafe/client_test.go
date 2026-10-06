package typesafe

import (
	"net/http"
	"strings"
	"testing"
	"time"
)

func clientEnvironment(t *testing.T) {
	t.Helper()
	for _, key := range []string{"TYPESAFE_API_KEY", "TYPESAFE_BASE_URL", "TYPESAFE_DEFAULT_MODEL"} {
		t.Setenv(key, "")
	}
}

func TestClientDefaultsAndEnvironment(t *testing.T) {
	clientEnvironment(t)
	if _, err := NewClient(ClientOptions{}); err == nil {
		t.Fatal("missing API key accepted")
	}
	c, err := NewClient(ClientOptions{APIKey: "  default-key\n"})
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	if c.apiKey != "default-key" || c.baseURL != DefaultBaseURL || c.defaultModel != DefaultModel || c.timeout != DefaultTimeout {
		t.Fatal("unexpected defaults")
	}
	t.Setenv("TYPESAFE_API_KEY", "env-key")
	t.Setenv("TYPESAFE_BASE_URL", "http://localhost:1234/prefix/")
	t.Setenv("TYPESAFE_DEFAULT_MODEL", "env-model")
	c, err = NewClient(ClientOptions{})
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	if c.apiKey != "env-key" || c.baseURL != "http://localhost:1234/prefix" || c.defaultModel != "env-model" {
		t.Fatal("environment fallback not applied")
	}
	c, err = NewClient(ClientOptions{APIKey: "explicit-key", BaseURL: "https://example.com", DefaultModel: "explicit-model", Timeout: time.Minute})
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	if c.apiKey != "explicit-key" || c.baseURL != "https://example.com" || c.defaultModel != "explicit-model" || c.timeout != time.Minute {
		t.Fatal("explicit settings did not override environment")
	}
}

func TestClientRejectsInvalidConfiguration(t *testing.T) {
	clientEnvironment(t)
	for _, key := range []string{" \t ", "secret key", "secret\nkey", "secret\x7fkey", "secretékey", "secret\x00key"} {
		if _, err := NewClient(ClientOptions{APIKey: key}); err == nil || strings.Contains(err.Error(), strings.TrimSpace(key)) && strings.TrimSpace(key) != "" {
			t.Errorf("invalid key accepted or disclosed: %q", key)
		}
	}
	for _, base := range []string{"/relative", "ftp://example.com", "https:///empty", "https://user:secret@example.com", "https://example.com?secret=x", "https://example.com?", "https://example.com#", "https://example.com#secret", "https://example.com:bad", "https://%invalid"} {
		if _, err := NewClient(ClientOptions{APIKey: "valid", BaseURL: base}); err == nil || strings.Contains(err.Error(), base) {
			t.Errorf("invalid URL accepted or disclosed: %q", base)
		}
	}
	if _, err := NewClient(ClientOptions{APIKey: "valid", Timeout: -time.Nanosecond}); err == nil {
		t.Fatal("negative timeout accepted")
	}
	if _, err := NewClient(ClientOptions{APIKey: "valid", DefaultModel: "  "}); err == nil {
		t.Fatal("blank model accepted")
	}
}

type closeTrackingTransport struct{ closed bool }

func (*closeTrackingTransport) RoundTrip(*http.Request) (*http.Response, error) {
	panic("configuration must not make HTTP requests")
}
func (tr *closeTrackingTransport) CloseIdleConnections() { tr.closed = true }

func TestClientIsolationAndOwnership(t *testing.T) {
	clientEnvironment(t)
	headers := http.Header{"X-Test": {"original"}}
	tr := &closeTrackingTransport{}
	injected := &http.Client{Transport: tr, Timeout: time.Minute}
	c, err := NewClient(ClientOptions{APIKey: "key", Headers: headers, HTTPClient: injected})
	if err != nil {
		t.Fatal(err)
	}
	headers["X-Test"][0] = "mutated"
	if c.headers.Get("X-Test") != "original" || c.httpClient != injected || injected.Timeout != time.Minute || c.timeout != DefaultTimeout {
		t.Fatal("caller-owned configuration mutated")
	}
	c.Close()
	c.Close()
	if tr.closed {
		t.Fatal("injected transport was closed")
	}
	a, err := NewClient(ClientOptions{APIKey: "one"})
	if err != nil {
		t.Fatal(err)
	}
	defer a.Close()
	b, err := NewClient(ClientOptions{APIKey: "two"})
	if err != nil {
		t.Fatal(err)
	}
	defer b.Close()
	if a.ownedTransport == nil || a.ownedTransport == b.ownedTransport || a.ownedTransport == http.DefaultTransport {
		t.Fatal("SDK connection pools are not isolated")
	}
	a.headers.Set("X-Test", "one")
	if b.headers.Get("X-Test") != "" {
		t.Fatal("SDK headers are shared")
	}
	var nilClient *Client
	if err := nilClient.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestClientIgnoresBlankEnvironmentDefaults(t *testing.T) {
	clientEnvironment(t)
	t.Setenv("TYPESAFE_BASE_URL", " \t\n ")
	t.Setenv("TYPESAFE_DEFAULT_MODEL", " \t\n ")
	c, err := NewClient(ClientOptions{APIKey: "key"})
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	if c.baseURL != DefaultBaseURL || c.defaultModel != DefaultModel {
		t.Fatal("blank environment values did not fall back to defaults")
	}
	if _, err := NewClient(ClientOptions{APIKey: "key", BaseURL: " \t\n "}); err == nil {
		t.Fatal("explicit blank URL accepted")
	}
	if _, err := NewClient(ClientOptions{APIKey: "key", DefaultModel: " \t\n "}); err == nil {
		t.Fatal("explicit blank model accepted")
	}
	t.Setenv("TYPESAFE_API_KEY", " \t\n ")
	if _, err := NewClient(ClientOptions{}); err == nil {
		t.Fatal("blank environment API key accepted")
	}
}

func TestClientRetryConfiguration(t *testing.T) {
	clientEnvironment(t)
	policy := DefaultRetryPolicy()
	c, err := NewClient(ClientOptions{APIKey: "key", Retry: &policy})
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	policy.MaxRetries, policy.HTTPStatuses[500] = 0, false
	if c.retry.MaxRetries != 2 || !c.retry.HTTPStatuses[500] {
		t.Fatal("client retained caller retry configuration")
	}
	other, err := NewClient(ClientOptions{APIKey: "key"})
	if err != nil {
		t.Fatal(err)
	}
	defer other.Close()
	c.retry.HTTPStatuses[500] = false
	if !other.retry.HTTPStatuses[500] {
		t.Fatal("default client status maps are shared")
	}
	disabled, err := NewClient(ClientOptions{APIKey: "key", Retry: &RetryPolicy{}})
	if err != nil {
		t.Fatal(err)
	}
	defer disabled.Close()
	if disabled.retry.MaxRetries != 0 || disabled.retry.ConnectionErrors || disabled.retry.HTTPStatuses != nil {
		t.Fatal("zero policy did not disable retries")
	}
	if invalid, err := NewClient(ClientOptions{APIKey: "key", Retry: &RetryPolicy{MaxRetries: -1}}); err == nil || invalid != nil {
		t.Fatal("invalid retry policy created a client")
	}
}
