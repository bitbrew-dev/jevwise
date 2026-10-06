package mcpserver

import (
	"bytes"
	"crypto/sha256"
	"crypto/subtle"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/netip"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

const HTTPHeaderLimit = 16 << 10
const httpConcurrency = 4

// ValidateAddress accepts only literal loopback IPs with a nonzero port.
func ValidateAddress(address string) (string, error) {
	parsed, err := netip.ParseAddrPort(address)
	if err != nil || !parsed.Addr().IsLoopback() || parsed.Addr().Zone() != "" || parsed.Port() == 0 {
		return "", errors.New("MCP address must be a loopback IP with a nonzero port")
	}
	return parsed.String(), nil
}

// NewHTTP protects /mcp only. The caller owns binding, server timeouts and shutdown.
// Set http.Server.MaxHeaderBytes to HTTPHeaderLimit before accepting connections.
func NewHTTP(server *mcp.Server, address, token string) (http.Handler, error) {
	address, err := ValidateAddress(address)
	if err != nil {
		return nil, err
	}
	if server == nil {
		return nil, errors.New("invalid MCP HTTP configuration")
	}
	if err := ValidateToken(token); err != nil {
		return nil, err
	}
	expected := sha256.Sum256([]byte("Bearer " + token))
	transport := mcp.NewStreamableHTTPHandler(func(*http.Request) *mcp.Server { return server }, &mcp.StreamableHTTPOptions{
		Stateless: true, PropagateRequestCancellation: true, MaxRequestBodyBytes: inputLimit,
		Logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
	})
	slots := make(chan struct{}, httpConcurrency)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Body != nil {
			defer r.Body.Close()
		}
		headerBytes := len(r.Host) + len(r.RequestURI)
		for name, values := range r.Header {
			for _, value := range values {
				headerBytes += len(name) + len(value) + 4
			}
		}
		status := 0
		switch {
		case headerBytes > HTTPHeaderLimit:
			status = http.StatusRequestHeaderFieldsTooLarge
		case r.Host != address:
			status = http.StatusForbidden
		case len(r.Header.Values("Origin")) > 1 || (len(r.Header.Values("Origin")) == 1 && r.Header.Get("Origin") != "http://"+address):
			status = http.StatusForbidden
		case r.URL.EscapedPath() != "/mcp" || r.URL.RawQuery != "" || r.URL.Fragment != "":
			status = http.StatusNotFound
		}
		authorization := r.Header.Get("Authorization")
		if scheme, credential, ok := strings.Cut(authorization, " "); ok && strings.EqualFold(scheme, "Bearer") {
			authorization = "Bearer " + credential
		}
		provided := sha256.Sum256([]byte(authorization))
		if status == 0 && (len(r.Header.Values("Authorization")) != 1 || subtle.ConstantTimeCompare(expected[:], provided[:]) != 1) {
			status = http.StatusUnauthorized
		}
		if status == 0 && r.Method != http.MethodPost {
			w.Header().Set("Allow", "POST")
			status = http.StatusMethodNotAllowed
		}
		if status != 0 {
			http.Error(w, "MCP request rejected", status)
			return
		}
		select {
		case slots <- struct{}{}:
			defer func() { <-slots }()
		default:
			http.Error(w, "MCP server is busy", http.StatusServiceUnavailable)
			return
		}
		if r.Body == nil {
			r.Body = http.NoBody
		}
		body, err := io.ReadAll(io.LimitReader(r.Body, inputLimit+1))
		if len(body) > inputLimit {
			http.Error(w, "MCP request rejected", http.StatusRequestEntityTooLarge)
			return
		}
		if err != nil || r.Context().Err() != nil {
			http.Error(w, "MCP request rejected", http.StatusBadRequest)
			return
		}
		// Even legacy revisions cannot batch paid decisions through one slot.
		if trimmed := bytes.TrimSpace(body); len(trimmed) > 0 && trimmed[0] == '[' {
			http.Error(w, "MCP request rejected", http.StatusBadRequest)
			return
		}
		r.Body = io.NopCloser(bytes.NewReader(body))
		r.ContentLength = int64(len(body))
		writer := &httpPrivacyWriter{ResponseWriter: w}
		transport.ServeHTTP(writer, r)
		writer.finish()
	}), nil
}

// ValidateToken applies the same agent credential rules before service creation.
func ValidateToken(token string) error {
	if token == "" || len(token) > 4096 {
		return errors.New("invalid MCP bearer token")
	}
	for _, char := range token {
		if char < 33 || char > 126 {
			return errors.New("invalid MCP bearer token")
		}
	}
	return nil
}
