// Package daemon provides authenticated control of an owned MCP lifetime.
package daemon

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"net/netip"
	"strconv"
	"sync"
)

const instanceHeader = "X-Jev-Instance"

// ManagementStatus identifies the exact instance and its startup phase.
type ManagementStatus struct {
	Instance string `json:"instance"`
	State    string `json:"state"`
}

// Management mutually authenticates control without transmitting its private key.
type Management struct {
	address  string
	instance string
	token    string
	cancel   context.CancelFunc
	mu       sync.Mutex
	state    string
}

// NewManagement prepares a private control handler for one owned lifetime.
func NewManagement(address, instance, controlToken string, cancel context.CancelFunc) (*Management, error) {
	if !controlAddress(address) || !controlID(instance, 16, 128) || !controlID(controlToken, 32, 256) || cancel == nil {
		return nil, errors.New("invalid management configuration")
	}
	return &Management{address: address, instance: instance, token: controlToken, cancel: cancel, state: "prepared"}, nil
}

func controlAddress(address string) bool {
	host, port, err := net.SplitHostPort(address)
	if err != nil {
		return false
	}
	ip, err := netip.ParseAddr(host)
	n, portErr := strconv.Atoi(port)
	return err == nil && ip.IsLoopback() && ip.Zone() == "" && portErr == nil && n > 0 && n <= 65535
}

func controlID(value string, min, max int) bool {
	if len(value) < min || len(value) > max {
		return false
	}
	for _, c := range value {
		if !(c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == '-' || c == '_') {
			return false
		}
	}
	return true
}

// MarkRunning acknowledges startup. It cannot revive a stopped instance.
func (m *Management) MarkRunning() error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.state != "prepared" {
		return errors.New("management instance is not prepared")
	}
	m.state = "running"
	return nil
}

// Handler serves only status and stop, with exact host, peer and instance checks.
func (m *Management) Handler() http.Handler {
	return http.HandlerFunc(m.serveHTTP)
}

func (m *Management) serveHTTP(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	peer, parseErr := netip.ParseAddr(host)
	origins := r.Header.Values("Origin")
	if r.Host != m.address || err != nil || parseErr != nil || !peer.IsLoopback() || peer.Zone() != "" || len(origins) > 1 || (len(origins) == 1 && origins[0] != "http://"+m.address) {
		managementError(w, http.StatusForbidden)
		return
	}
	nonces := r.Header.Values(nonceHeader)
	proofs := r.Header.Values(proofHeader)
	instances := r.Header.Values(instanceHeader)
	if len(r.Header.Values("Authorization")) != 0 || len(nonces) != 1 || !validControlHex(nonces[0]) || len(proofs) != 1 || !validControlHex(proofs[0]) {
		w.Header().Set("WWW-Authenticate", "Jev-HMAC")
		managementError(w, http.StatusUnauthorized)
		return
	}
	if len(instances) != 1 || subtle.ConstantTimeCompare([]byte(instances[0]), []byte(m.instance)) != 1 {
		managementError(w, http.StatusConflict)
		return
	}
	if !verifiedProof(proofs[0], requestProof(m.token, r.Method, r.URL.EscapedPath(), m.instance, nonces[0])) {
		managementError(w, http.StatusUnauthorized)
		return
	}
	path := r.URL.EscapedPath()
	if r.URL.RawQuery != "" || (path != "/_jev/status" && path != "/_jev/stop") {
		managementError(w, http.StatusNotFound)
		return
	}
	method := http.MethodGet
	if path == "/_jev/stop" {
		method = http.MethodPost
	}
	if r.Method != method {
		w.Header().Set("Allow", method)
		managementError(w, http.StatusMethodNotAllowed)
		return
	}
	if r.ContentLength != 0 || len(r.TransferEncoding) != 0 {
		managementError(w, http.StatusBadRequest)
		return
	}
	m.mu.Lock()
	stop := path == "/_jev/stop" && m.state != "stopping"
	if stop {
		m.state = "stopping"
	}
	status := ManagementStatus{Instance: m.instance, State: m.state}
	m.mu.Unlock()
	if stop {
		defer m.cancel()
	}
	w.Header().Set(proofHeader, responseProof(m.token, r.Method, path, m.instance, nonces[0], status.State))
	body, _ := json.Marshal(status) // This concrete response contains only strings.
	body = append(body, '\n')
	w.Header().Set("Content-Length", strconv.Itoa(len(body)))
	_, _ = w.Write(body)
	if stop {
		// Flush the acknowledgement before cancellation can shut down the listener.
		_ = http.NewResponseController(w).Flush()
	}
}

func managementError(w http.ResponseWriter, status int) {
	w.WriteHeader(status)
	_, _ = w.Write([]byte("{\"error\":\"management request rejected\"}\n"))
}

// IsRunning admits decisions only after ACK and before a stop request.
func (m *Management) IsRunning() bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.state == "running"
}
