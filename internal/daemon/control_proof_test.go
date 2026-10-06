package daemon

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestControlProofDomainsAndNonces(t *testing.T) {
	if got := requestProof(testControlToken, "GET", "/_jev/status", testInstance, strings.Repeat("a", 64)); got != "eff0e04dfec9dcba9e36ec1f13892ef20405ca21468be4bff72a619d0725f1d2" {
		t.Fatal("HMAC-SHA256 proof vector changed")
	}
	nonce, err := newControlNonce()
	if err != nil || !validControlHex(nonce) {
		t.Fatal("nonce creation failed", err)
	}
	next, err := newControlNonce()
	if err != nil || !validControlHex(next) || nonce == next {
		t.Fatal("nonce was reused", err)
	}
	request := requestProof(testControlToken, "GET", "/_jev/status", testInstance, nonce)
	response := responseProof(testControlToken, "GET", "/_jev/status", testInstance, nonce, "running")
	if !verifiedProof(request, request) || verifiedProof(request, response) {
		t.Fatal("proof domains are interchangeable")
	}
	for _, changed := range []string{
		requestProof("wrong-key", "GET", "/_jev/status", testInstance, nonce),
		requestProof(testControlToken, "POST", "/_jev/status", testInstance, nonce),
		requestProof(testControlToken, "GET", "/_jev/stop", testInstance, nonce),
		requestProof(testControlToken, "GET", "/_jev/status", "other-instance", nonce),
		requestProof(testControlToken, "GET", "/_jev/status", testInstance, next),
		responseProof(testControlToken, "GET", "/_jev/status", testInstance, nonce, "prepared"),
		responseProof(testControlToken, "POST", "/_jev/status", testInstance, nonce, "running"),
		responseProof(testControlToken, "GET", "/_jev/stop", testInstance, nonce, "running"),
		responseProof(testControlToken, "GET", "/_jev/status", testInstance, next, "running"),
	} {
		if verifiedProof(changed, request) || verifiedProof(changed, response) {
			t.Fatal("changed proof bindings authenticated")
		}
	}
	for _, invalid := range []string{"", strings.Repeat("a", 63), strings.Repeat("a", 65), strings.Repeat("A", 64), strings.Repeat("g", 64)} {
		if validControlHex(invalid) || verifiedProof(invalid, request) {
			t.Fatal("unbounded or noncanonical proof accepted")
		}
	}
}

func TestManagementRejectsForgedRequests(t *testing.T) {
	for name, edit := range map[string]func(*http.Request){
		"wrong key": func(r *http.Request) {
			r.Header.Set(proofHeader, requestProof("wrong-key", r.Method, r.URL.EscapedPath(), testInstance, r.Header.Get(nonceHeader)))
		},
		"invalid nonce":  func(r *http.Request) { r.Header.Set(nonceHeader, "invalid") },
		"missing nonce":  func(r *http.Request) { r.Header.Del(nonceHeader) },
		"double nonce":   func(r *http.Request) { r.Header.Add(nonceHeader, r.Header.Get(nonceHeader)) },
		"changed nonce":  func(r *http.Request) { r.Header.Set(nonceHeader, strings.Repeat("b", 64)) },
		"changed method": func(r *http.Request) { r.Method = "GET" },
		"changed path":   func(r *http.Request) { r.URL.Path = "/_jev/status" },
		"response reflection": func(r *http.Request) {
			r.Header.Set(proofHeader, responseProof(testControlToken, r.Method, r.URL.EscapedPath(), testInstance, r.Header.Get(nonceHeader), "stopping"))
		},
	} {
		t.Run(name, func(t *testing.T) {
			m := testManagement(t, func() { t.Error("forgery canceled lifetime") })
			r := managementRequest("POST", "/_jev/stop")
			edit(r)
			w := httptest.NewRecorder()
			m.Handler().ServeHTTP(w, r)
			if w.Code != 401 || w.Header().Get(proofHeader) != "" || strings.Contains(w.Body.String(), testControlToken) {
				t.Fatalf("forged response = %d %q", w.Code, w.Body.String())
			}
			if m.MarkRunning() != nil {
				t.Fatal("forgery changed instance phase")
			}
		})
	}
}

func TestControllerRefusesPortImpersonation(t *testing.T) {
	for _, attack := range []string{"no proof", "reflection", "wrong key", "wrong nonce", "duplicate", "replay", "valid"} {
		t.Run(attack, func(t *testing.T) {
			previousProof, previousNonce := "", ""
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				for _, values := range r.Header {
					for _, value := range values {
						if strings.Contains(value, controllerToken) {
							t.Error("raw management key was transmitted")
						}
					}
				}
				if r.Header.Get("Authorization") != "" || !validControlHex(r.Header.Get(nonceHeader)) {
					t.Error("invalid proof-only request")
				}
				nonce := r.Header.Get(nonceHeader)
				proof := responseProof(controllerToken, r.Method, r.URL.EscapedPath(), controllerInstance, nonce, "running")
				switch attack {
				case "no proof":
					proof = ""
				case "reflection":
					proof = r.Header.Get(proofHeader)
				case "wrong key":
					proof = responseProof("wrong-key", r.Method, r.URL.EscapedPath(), controllerInstance, nonce, "running")
				case "wrong nonce":
					proof = responseProof(controllerToken, r.Method, r.URL.EscapedPath(), controllerInstance, strings.Repeat("b", 64), "running")
				case "replay":
					if previousProof != "" {
						if nonce == previousNonce {
							t.Error("controller reused nonce")
						}
						proof = previousProof
					}
					previousProof, previousNonce = proof, nonce
				}
				w.Header().Set("Content-Type", "application/json")
				if proof != "" {
					w.Header().Set(proofHeader, proof)
				}
				if attack == "duplicate" {
					w.Header().Add(proofHeader, proof)
				}
				_, _ = w.Write([]byte(statusJSON("running")))
			}))
			defer server.Close()
			c := controllerFor(t, server)
			if attack == "replay" {
				if _, err := c.Status(context.Background()); err != nil {
					t.Fatal("initial signed response failed", err)
				}
			}
			status, err := c.Status(context.Background())
			if attack == "valid" {
				if err != nil || status.State != "running" {
					t.Fatal("valid response rejected", err)
				}
			} else if err == nil || err.Error() != "management request failed" || status != (ManagementStatus{}) {
				t.Fatalf("forged status = %+v, %v", status, err)
			}
		})
	}
}
