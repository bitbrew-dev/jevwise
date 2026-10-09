package config

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestInitPrivateTemplateNoOverwriteOrCredentialCopy(t *testing.T) {
	cleanEnv(t)
	t.Setenv("TYPESAFE_API_KEY", "environment-secret")
	path := filepath.Join(t.TempDir(), "new", "config.toml")
	if err := Init(context.Background(), path); err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(path)
	info, _ := os.Stat(path)
	parent, _ := os.Stat(filepath.Dir(path))
	cfg, err := Load(path, nil)
	if err != nil || cfg.Model != "jev-latest" || strings.Contains(string(data), "environment-secret") {
		t.Fatal("template invalid or copied credentials", err)
	}
	if runtime.GOOS != "windows" && (info.Mode().Perm() != 0600 || parent.Mode().Perm() != 0700) {
		t.Fatal("template or new parent is not private")
	}
	if err := Init(context.Background(), path); !errors.Is(err, os.ErrExist) {
		t.Fatal("existing file accepted", err)
	}
	after, _ := os.ReadFile(path)
	entries, _ := os.ReadDir(filepath.Dir(path))
	if string(after) != string(data) || len(entries) != 1 {
		t.Fatal("existing file changed or staging leaked")
	}
	for _, kind := range []string{"directory", "link"} {
		target := filepath.Join(filepath.Dir(path), kind)
		if kind == "link" {
			if runtime.GOOS == "windows" {
				continue
			}
			if err := os.Symlink(path, target); err != nil {
				t.Fatal(err)
			}
		} else {
			_ = os.Mkdir(target, 0700)
		}
		if err := Init(context.Background(), target); err == nil {
			t.Fatal("existing entry replaced")
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	missing := filepath.Join(t.TempDir(), "absent", "config.toml")
	if err := Init(ctx, missing); !errors.Is(err, context.Canceled) {
		t.Fatal("cancellation ignored", err)
	}
	if _, err := os.Stat(filepath.Dir(missing)); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("cancelled init changed storage")
	}
}

func TestViewOnlyKnownFileValuesAndRedactsSecrets(t *testing.T) {
	cleanEnv(t)
	t.Setenv("TS_JEV_MODEL", "environment-model")
	path := writeConfig(t, filepath.Join(t.TempDir(), "config.toml"), "API_KEY='file-secret'\nmodel='file-model'\nextra='extra-secret'\n# comment-secret\n")
	text, err := View(path)
	if err != nil || !strings.Contains(text, `model = "file-model"`) || !strings.Contains(text, `editor = "vim"`) || !strings.Contains(text, "[redacted]") {
		t.Fatal("wrong file view", err)
	}
	for _, secret := range []string{"file-secret", "extra-secret", "comment-secret", "environment-model"} {
		if strings.Contains(text, secret) {
			t.Fatal("view leaked non-file or secret data")
		}
	}
	for _, content := range []string{"api_key='parse-secret'\n[", "base_url='https://user:url-secret@example.com'", "editor=42"} {
		writeConfig(t, path, content)
		if out, err := View(path); out != "" || err == nil || strings.Contains(err.Error(), "secret") {
			t.Fatal("invalid file accepted or leaked", err)
		}
	}
}

func TestReadFileBoundaryAndExplicitReadOnlyLink(t *testing.T) {
	cleanEnv(t)
	path := writeConfig(t, filepath.Join(t.TempDir(), "config.toml"), "#"+strings.Repeat("x", MaxFileBytes-2)+"\n")
	if data, err := ReadFile(path); err != nil || len(data) != MaxFileBytes {
		t.Fatal("exact boundary refused", err)
	}
	if _, err := Load(path, nil); err != nil {
		t.Fatal("boundary TOML refused", err)
	}
	if runtime.GOOS != "windows" {
		link := path + ".link"
		if err := os.Symlink(path, link); err != nil {
			t.Fatal(err)
		}
		if _, err := Load(link, nil); err != nil {
			t.Fatal("explicit read-only link compatibility lost", err)
		}
	}
}
