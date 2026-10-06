package daemon

import (
	"bytes"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/bitbrew-dev/jevwise/internal/config"
)

func validBootstrap(t *testing.T) Bootstrap {
	t.Helper()
	return Bootstrap{Config: config.Config{APIKey: "private-upstream-key", BaseURL: "http://127.0.0.1:1/api", Model: "fixture-model", Provider: "jev", Timeout: time.Second},
		Address: "127.0.0.1:8080", AgentToken: "private-agent-token", RuntimeDir: filepath.Join(t.TempDir(), "not-created"),
		Instance: strings.Repeat("a", 32), ControlToken: strings.Repeat("b", 64)}
}

func TestBootstrapValidationIsPure(t *testing.T) {
	boot := validBootstrap(t)
	before := boot
	if err := ValidateBootstrap(boot); err != nil || boot != before {
		t.Fatal("valid bootstrap changed or failed", err)
	}
	if _, err := os.Lstat(boot.RuntimeDir); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("validation created runtime storage")
	}
	boot.Address = "[::1]:8080"
	if err := ValidateBootstrap(boot); err != nil {
		t.Fatal("canonical IPv6 loopback rejected", err)
	}
}

func TestBootstrapValidationRejectsUnsafeValues(t *testing.T) {
	valid := validBootstrap(t)
	tests := []struct {
		name   string
		change func(*Bootstrap)
	}{
		{"missing API key", func(b *Bootstrap) { b.Config.APIKey = "" }},
		{"placeholder provider", func(b *Bootstrap) { b.Config.Provider = "claude" }},
		{"blank model", func(b *Bootstrap) { b.Config.Model = " " }},
		{"invalid endpoint", func(b *Bootstrap) { b.Config.BaseURL = "https://private-user@fixture.test" }},
		{"zero timeout", func(b *Bootstrap) { b.Config.Timeout = 0 }},
		{"overflow timeout", func(b *Bootstrap) { b.Config.Timeout = time.Duration(1<<63 - 1) }},
		{"key whitespace", func(b *Bootstrap) { b.Config.APIKey += " " }},
		{"URL whitespace", func(b *Bootstrap) { b.Config.BaseURL += " " }},
		{"model whitespace", func(b *Bootstrap) { b.Config.Model += " " }},
		{"invalid model UTF8", func(b *Bootstrap) { b.Config.Model = "private-\xff" }},
		{"missing agent token", func(b *Bootstrap) { b.AgentToken = "" }},
		{"agent token whitespace", func(b *Bootstrap) { b.AgentToken += "\n" }},
		{"nonliteral address", func(b *Bootstrap) { b.Address = "localhost:8080" }},
		{"public address", func(b *Bootstrap) { b.Address = "0.0.0.0:8080" }},
		{"zero port", func(b *Bootstrap) { b.Address = "127.0.0.1:0" }},
		{"noncanonical address", func(b *Bootstrap) { b.Address = "[0:0:0:0:0:0:0:1]:8080" }},
		{"relative runtime path", func(b *Bootstrap) { b.RuntimeDir = "private-directory" }},
		{"unclean runtime path", func(b *Bootstrap) { b.RuntimeDir += string(filepath.Separator) + "." }},
		{"invalid runtime UTF8", func(b *Bootstrap) { b.RuntimeDir += "\xff" }},
		{"short instance", func(b *Bootstrap) { b.Instance = "private" }},
		{"invalid instance", func(b *Bootstrap) { b.Instance = strings.Repeat("a", 31) + "!" }},
		{"short control key", func(b *Bootstrap) { b.ControlToken = "private" }},
		{"invalid control key", func(b *Bootstrap) { b.ControlToken = strings.Repeat("b", 63) + " " }},
		{"agent equals upstream", func(b *Bootstrap) { b.AgentToken = b.Config.APIKey }},
		{"agent equals control", func(b *Bootstrap) { b.AgentToken = b.ControlToken }},
		{"control equals upstream", func(b *Bootstrap) { b.Config.APIKey = b.ControlToken }},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			boot := valid
			test.change(&boot)
			before := boot
			if err := ValidateBootstrap(boot); err == nil || strings.Contains(err.Error(), "private") || boot != before {
				t.Fatal("unsafe bootstrap validation", err)
			}
		})
	}
	if err := ValidateBootstrap(Bootstrap{}); err == nil {
		t.Fatal("zero bootstrap accepted")
	}
}

type identityFailure struct{}

func (identityFailure) Read([]byte) (int, error) { return 0, errors.New("private-entropy-error") }

func TestFreshIdentityAndSafeEntropyFailure(t *testing.T) {
	instance, control, err := NewIdentity()
	if err != nil || len(instance) != 32 || len(control) != 64 || !controlID(instance, 16, 128) || !controlID(control, 32, 256) {
		t.Fatal("invalid generated identity", err)
	}
	other, key, err := NewIdentity()
	if err != nil || other == instance || key == control {
		t.Fatal("identity was reused", err)
	}
	instance, control, err = newIdentity(bytes.NewReader(bytes.Repeat([]byte{0xab}, 48)))
	if err != nil || instance != strings.Repeat("ab", 16) || control != strings.Repeat("ab", 32) {
		t.Fatal("entropy was not assigned correctly", err)
	}
	for _, source := range []io.Reader{nil, identityFailure{}, strings.NewReader("short")} {
		instance, control, err := newIdentity(source)
		if err == nil || instance != "" || control != "" || strings.Contains(err.Error(), "private") {
			t.Fatal("entropy failure leaked or returned partial identity", err)
		}
	}
}
