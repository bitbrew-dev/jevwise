package daemon

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"io"
	"path/filepath"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/bitbrew-dev/jevwise/internal/mcpserver"
)

// ValidateBootstrap checks already resolved startup values without accessing
// configuration files, environment, runtime storage, networks or processes.
// Runtime-directory privacy and exclusive ownership are checked during startup.
func ValidateBootstrap(boot Bootstrap) error {
	invalid := errors.New("invalid background bootstrap configuration")
	if boot.Config.Validate() != nil || boot.Config.Provider != "jev" || boot.Config.APIKey == "" || boot.Config.Timeout > time.Duration(1<<63-1)-30*time.Second {
		return invalid
	}
	for _, value := range []string{boot.Config.APIKey, boot.Config.BaseURL, boot.Config.Model, boot.Config.Provider} {
		if !utf8.ValidString(value) || strings.TrimSpace(value) != value {
			return invalid
		}
	}
	address, err := mcpserver.ValidateAddress(boot.Address)
	if err != nil || address != boot.Address || mcpserver.ValidateToken(boot.AgentToken) != nil {
		return invalid
	}
	if !utf8.ValidString(boot.RuntimeDir) || !filepath.IsAbs(boot.RuntimeDir) || filepath.Clean(boot.RuntimeDir) != boot.RuntimeDir {
		return invalid
	}
	// The pure management constructor also validates instance and control IDs.
	if _, err := NewManagement(address, boot.Instance, boot.ControlToken, func() {}); err != nil {
		return invalid
	}
	if boot.AgentToken == boot.Config.APIKey || boot.AgentToken == boot.ControlToken || boot.ControlToken == boot.Config.APIKey {
		return invalid
	}
	return nil
}

// NewIdentity creates a fresh public instance ID and a separate management key.
// These are not upstream credentials or the agent's independently supplied token.
func NewIdentity() (instance, controlToken string, err error) {
	return newIdentity(rand.Reader)
}

func newIdentity(random io.Reader) (string, string, error) {
	var entropy [48]byte
	if random == nil {
		return "", "", errors.New("cannot generate background instance identity")
	}
	if _, err := io.ReadFull(random, entropy[:]); err != nil {
		return "", "", errors.New("cannot generate background instance identity")
	}
	return hex.EncodeToString(entropy[:16]), hex.EncodeToString(entropy[16:]), nil
}
