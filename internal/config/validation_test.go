package config

import (
	"strings"
	"testing"
	"time"
)

func TestResolvedValidationIsPureAndCredentialsOptional(t *testing.T) {
	for _, provider := range []string{"jev", "codex", "claude"} {
		for _, key := range []string{"", "private-api-key"} {
			cfg := Config{APIKey: key, BaseURL: "http://127.0.0.1:8080/api", Model: "fixture-model", Provider: provider, Timeout: time.Second}
			before := cfg
			if err := cfg.Validate(); err != nil || cfg != before {
				t.Fatal("valid configuration changed or failed", err)
			}
		}
	}
}

func TestResolvedValidationRejectsInvalidValuesSafely(t *testing.T) {
	valid := Config{APIKey: "private-api-key", BaseURL: "https://fixture.test", Model: "fixture-model", Provider: "jev", Timeout: time.Second}
	tests := []struct {
		name   string
		change func(*Config)
	}{
		{"zero timeout", func(c *Config) { c.Timeout = 0 }},
		{"negative timeout", func(c *Config) { c.Timeout = -time.Second }},
		{"empty URL", func(c *Config) { c.BaseURL = "" }},
		{"relative URL", func(c *Config) { c.BaseURL = "private-path" }},
		{"URL credentials", func(c *Config) { c.BaseURL = "https://private-user:private-key@fixture.test" }},
		{"URL query", func(c *Config) { c.BaseURL = "https://fixture.test?key=private-value" }},
		{"URL fragment", func(c *Config) { c.BaseURL = "https://fixture.test#private-value" }},
		{"URL scheme", func(c *Config) { c.BaseURL = "file:///private-path" }},
		{"URL malformed", func(c *Config) { c.BaseURL = "https://private-%" }},
		{"empty model", func(c *Config) { c.Model = "" }},
		{"blank model", func(c *Config) { c.Model = " \t\n" }},
		{"unknown provider", func(c *Config) { c.Provider = "private-provider" }},
		{"provider case", func(c *Config) { c.Provider = "Jev" }},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			cfg := valid
			test.change(&cfg)
			before := cfg
			err := cfg.Validate()
			if err == nil || strings.Contains(err.Error(), "private") || cfg != before {
				t.Fatal("invalid configuration was accepted, leaked or changed", err)
			}
		})
	}
	if err := (Config{}).Validate(); err == nil {
		t.Fatal("zero configuration accepted")
	}
}
