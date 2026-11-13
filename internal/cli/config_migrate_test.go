package cli

import (
	"context"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/bitbrew-dev/jevwise/internal/config"
)

func TestConfigMigrateCommandAndFlatHints(t *testing.T) {
	configEnvironment(t)
	path := filepath.Join(t.TempDir(), "config.toml")
	if err := os.WriteFile(path, []byte("api_key = 'private-key'\nmcp_port = 9090\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, _, err := execute(configRoot(t), "mcp", "--config", path); err == nil || !strings.Contains(err.Error(), "jevwise config migrate") || strings.Contains(err.Error(), "private") {
		t.Fatal("flat file not rejected with migrate hint", err)
	}
	if out, _, err := execute(NewRoot(), "version", "--config", path); err != nil || !strings.Contains(out, "commit:") {
		t.Fatal("version failed on a flat file", err)
	}
	if _, _, err := execute(NewRoot(), "mcp", "status", "--runtime-dir", t.TempDir(), "--config", path); err != nil && strings.Contains(err.Error(), "migrate") {
		t.Fatal("mcp status loaded the flat file", err)
	}
	edited := ""
	edit := newConfigEdit(func(_ context.Context, editor, target string, _ io.Reader, _, _ io.Writer) error {
		edited = editor + " " + target
		return nil
	})
	root := configRoot(t)
	configCmd, _, _ := root.Find([]string{"config"})
	for _, child := range configCmd.Commands() {
		if child.Name() == "edit" {
			configCmd.RemoveCommand(child)
		}
	}
	configCmd.AddCommand(edit)
	if _, _, err := execute(root, "config", "edit", "--config", path); err != nil || edited != "vim "+path {
		t.Fatal("config edit cannot open a flat file", edited, err)
	}
	out, _, err := execute(configRoot(t), "config", "migrate", "--config", path)
	if err != nil || !strings.Contains(out, "Migrated configuration") || !strings.Contains(out, config.BackupSuffix) || strings.Contains(out, "private") {
		t.Fatal("migrate output wrong or leaked", out, err)
	}
	if view, _, err := execute(configRoot(t), "config", "view", "--config", path); err != nil || !strings.Contains(view, "[mcp]\nhost = \"127.0.0.1\"\nport = 9090\n") || strings.Contains(view, "private") {
		t.Fatal("migrated view wrong or leaked", view, err)
	}
	if out, _, err := execute(configRoot(t), "config", "migrate", "--config", path); err != nil || !strings.Contains(out, "nothing changed") {
		t.Fatal("second migration not a no-op", out, err)
	}
}
