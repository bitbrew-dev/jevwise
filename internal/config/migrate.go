package config

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"

	"github.com/spf13/viper"
)

// ErrNotFlat reports a file that already uses grouped tables; nothing is changed.
var ErrNotFlat = errors.New("configuration already uses grouped tables; nothing to migrate")

// BackupSuffix names the private copy Migrate keeps beside the original file.
const BackupSuffix = ".flat.bak"

type target struct{ section, key string }

var migration = map[string]target{
	"provider": {"general", "provider"}, "debug": {"general", "debug"}, "editor": {"general", "editor"},
	"mcp_base_url": {"mcp", "host"}, "mcp_port": {"mcp", "port"},
	"api_key": {"provider", "api_key"}, "base_url": {"provider", "base_url"}, "model": {"provider", "model"}, "timeout": {"provider", "timeout"},
}

// Migrate rewrites a flat configuration file as grouped tables and returns the
// backup path. It moves keys without validating or expanding values, so $NAME
// references stay literal and invalid values can be fixed afterwards. Comments are
// dropped. Links, unknown or mixed keys, existing backups and values other than
// strings, integers or booleans are refused. Errors name keys, never values.
func Migrate(ctx context.Context, path string) (string, error) {
	if ctx == nil {
		return "", errors.New("configuration context is required")
	}
	if err := ctx.Err(); err != nil {
		return "", err
	}
	if info, err := os.Lstat(path); err != nil {
		return "", errors.New("cannot read configuration file; nothing to migrate")
	} else if !info.Mode().IsRegular() {
		return "", errors.New("configuration must be a regular file, not a link; migrate the link target instead")
	}
	data, err := ReadFile(path)
	if err != nil {
		return "", errors.New("cannot read configuration file; nothing to migrate")
	}
	v, err := parseTOML(data)
	if err != nil {
		return "", err
	}
	if !isFlat(v) {
		return "", ErrNotFlat
	}
	text, err := groupedTOML(v)
	if err != nil {
		return "", err
	}
	if err := ctx.Err(); err != nil {
		return "", err
	}
	return path + BackupSuffix, replaceWithBackup(path, data, text)
}

func groupedTOML(v *viper.Viper) ([]byte, error) {
	provider := "jev"
	if name, ok := v.Get("provider").(string); ok && slices.Contains(Providers, strings.TrimSpace(name)) {
		provider = strings.TrimSpace(name)
	}
	lines := map[string][]string{}
	for _, key := range v.AllKeys() {
		to, ok := migration[key]
		if !ok {
			return nil, fmt.Errorf("cannot migrate %q: unknown or mixed setting; move it by hand", key)
		}
		value, err := tomlValue(v.Get(key))
		if err != nil {
			return nil, fmt.Errorf("cannot migrate %q: value must be a string, integer or boolean", key)
		}
		lines[to.section] = append(lines[to.section], to.key+" = "+value)
	}
	var out bytes.Buffer
	out.WriteString("# Migrated from the flat layout; comments were not carried over.\n")
	for _, section := range []string{"general", "mcp", "provider"} {
		entries := lines[section]
		if len(entries) == 0 {
			continue
		}
		slices.SortFunc(entries, func(a, b string) int { return order(a) - order(b) })
		name := section
		if section == "provider" {
			name = "provider." + provider
		}
		out.WriteString("\n[" + name + "]\n" + strings.Join(entries, "\n") + "\n")
	}
	return out.Bytes(), nil
}

func order(line string) int {
	key, _, _ := strings.Cut(line, " ")
	return slices.Index([]string{"provider", "debug", "editor", "host", "port", "api_key", "base_url", "model", "timeout"}, key)
}

func tomlValue(raw any) (string, error) {
	switch value := raw.(type) {
	case string:
		var buf bytes.Buffer
		encoder := json.NewEncoder(&buf)
		encoder.SetEscapeHTML(false)
		if err := encoder.Encode(value); err != nil {
			return "", err
		}
		return strings.TrimSuffix(buf.String(), "\n"), nil
	case bool:
		return strconv.FormatBool(value), nil
	case int64:
		return strconv.FormatInt(value, 10), nil
	case int:
		return strconv.Itoa(value), nil
	}
	return "", errors.New("unsupported value")
}

// replaceWithBackup keeps the original bytes in a new private backup, then
// atomically renames a staged private file over the configuration. A failed
// rewrite removes its backup so the unchanged original can be migrated again.
func replaceWithBackup(path string, original, grouped []byte) (err error) {
	root, err := os.OpenRoot(filepath.Dir(path))
	if err != nil {
		return err
	}
	defer root.Close()
	name := filepath.Base(path)
	if err := writePrivate(root, name+BackupSuffix, original); err != nil {
		if errors.Is(err, os.ErrExist) {
			return errors.New("a migration backup already exists; move it away before migrating again")
		}
		return err
	}
	stage := ".config-" + rand.Text() + ".tmp"
	defer func() {
		if cleanup := root.Remove(stage); cleanup != nil && !errors.Is(cleanup, os.ErrNotExist) {
			err = errors.Join(err, errors.New("cannot clean configuration staging file"))
		}
		if err != nil {
			_ = root.Remove(name + BackupSuffix)
		}
	}()
	if err := writePrivate(root, stage, grouped); err != nil {
		return err
	}
	return root.Rename(stage, name)
}

func writePrivate(root *os.Root, name string, data []byte) error {
	file, err := root.OpenFile(name, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return err
	}
	if _, err := file.Write(data); err != nil {
		_ = file.Close()
		return err
	}
	if err := file.Sync(); err != nil {
		_ = file.Close()
		return err
	}
	return file.Close()
}
