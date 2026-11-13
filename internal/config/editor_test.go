package config

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestEditorUsesOnlyFileAndCanRepairInvalidTOML(t *testing.T) {
	cleanEnv(t)
	t.Setenv("EDITOR", "environment-editor")
	t.Setenv("VISUAL", "environment-visual")
	t.Setenv("TS_JEV_EDITOR", "environment-editor")
	for _, tc := range []struct {
		content, want string
		invalid       bool
	}{
		{"", "vim", false}, {"[general]\neditor='nvim'", "nvim", false}, {"[general]\neditor=' '", "vim", false},
		{"[general]\neditor='/some/editor with spaces'", "/some/editor with spaces", false},
		{"[general]\neditor='nano'\n[provider.jev]\nmodel=42", "nano", false}, {"[provider.jev]\napi_key='private-secret'\n[", "vim", false},
		{"[general]\neditor=42", "", true},
	} {
		path := writeConfig(t, filepath.Join(t.TempDir(), "config.toml"), tc.content)
		got, err := Editor(path)
		if got != tc.want || (err != nil) != tc.invalid || err != nil && strings.Contains(err.Error(), "private-secret") {
			t.Fatal("editor selection or safe error failed", err)
		}
	}
}
