package daemon

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"strings"
)

const nonceHeader = "X-Jev-Control-Nonce"
const proofHeader = "X-Jev-Control-Proof"

func newControlNonce() (string, error) {
	var nonce [32]byte
	if _, err := rand.Read(nonce[:]); err != nil {
		return "", err
	}
	return hex.EncodeToString(nonce[:]), nil
}

func validControlHex(value string) bool {
	if len(value) != 64 {
		return false
	}
	for _, c := range value {
		if !(c >= '0' && c <= '9' || c >= 'a' && c <= 'f') {
			return false
		}
	}
	return true
}

func controlProof(key, domain string, fields ...string) string {
	mac := hmac.New(sha256.New, []byte(key))
	_, _ = mac.Write([]byte(domain + "\n" + strings.Join(fields, "\n")))
	return hex.EncodeToString(mac.Sum(nil))
}

func requestProof(key, method, path, instance, nonce string) string {
	return controlProof(key, "jevwise-control-request-v1", method, path, instance, nonce)
}

func responseProof(key, method, path, instance, nonce, state string) string {
	return controlProof(key, "jevwise-control-response-v1", method, path, instance, nonce, state)
}

func verifiedProof(value, expected string) bool {
	return validControlHex(value) && hmac.Equal([]byte(value), []byte(expected))
}
