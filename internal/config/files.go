package config

import (
	"cmp"
	"context"
	"crypto/rand"
	_ "embed"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
)

//go:embed template.toml
var template []byte

// Init publishes a mode-restricted template without copying environment credentials or
// replacing any existing entry. Root confinement assumes trusted parent storage.
func Init(ctx context.Context, path string) (err error) {
	if ctx == nil {
		return errors.New("configuration context is required")
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	parent := filepath.Dir(path)
	if err := os.MkdirAll(parent, 0700); err != nil {
		return err
	}
	root, err := os.OpenRoot(parent)
	if err != nil {
		return err
	}
	defer root.Close()
	stage := ".config-" + rand.Text() + ".tmp"
	file, err := root.OpenFile(stage, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return err
	}
	published := false
	defer func() {
		_ = file.Close()
		if cleanup := root.Remove(stage); cleanup != nil {
			if published {
				err = errors.New("configuration created but staging cleanup failed")
			} else {
				err = errors.Join(err, errors.New("cannot clean configuration staging file"))
			}
		}
	}()
	if n, err := file.Write(template); err != nil {
		return err
	} else if n != len(template) {
		return io.ErrShortWrite
	}
	if err := file.Sync(); err != nil {
		return err
	}
	if err := file.Close(); err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := root.Link(stage, filepath.Base(path)); err != nil {
		return err
	}
	published = true
	return nil
}

func fileValues(path string) (map[string]string, error) {
	data, err := ReadFile(path)
	if err != nil {
		return nil, errors.New("cannot read configuration file; initialize it first")
	}
	v, err := readSettings(data)
	if err != nil {
		return nil, err
	}
	provider, _, err := fileString(v, "general.provider")
	if err != nil {
		return nil, err
	}
	values := defaultValues(cmp.Or(provider, "jev"))
	values["editor"] = "vim"
	set := func(key, path string) error {
		value, ok, err := fileString(v, path)
		if ok {
			values[key] = value
		}
		return err
	}
	if err := set("provider", "general.provider"); err != nil {
		return nil, err
	}
	if !slices.Contains(Providers, values["provider"]) {
		return nil, errProvider
	}
	for _, key := range []string{"api_key", "base_url", "model", "timeout"} {
		if err := set(key, "provider."+values["provider"]+"."+key); err != nil {
			return nil, err
		}
	}
	if err := set("editor", "general.editor"); err != nil {
		return nil, err
	}
	mcp, err := mcpValues(v, false)
	if err != nil {
		return nil, err
	}
	values["host"], values["port"] = mcp["host"], mcp["port"]
	debug, err := debugValue(v)
	if err != nil {
		return nil, err
	}
	values["debug"] = strconv.FormatBool(debug)
	return values, nil
}

// View shows known file values/defaults as grouped tables, never environment
// overrides, arbitrary extra keys, other provider tables or comments. Only api_key
// is redacted; URL userinfo/query/fragment are refused before display. Other
// settings must not contain credentials.
func View(path string) (string, error) {
	values, err := fileValues(path)
	if err != nil {
		return "", err
	}
	if _, err := fromValues(values); err != nil {
		return "", err
	}
	values["api_key"] = "[redacted]"
	var out strings.Builder
	for i, section := range []struct {
		name string
		keys []string
	}{
		{"general", []string{"provider", "debug", "editor"}},
		{"mcp", []string{"host", "port"}},
		{"provider." + values["provider"], []string{"api_key", "base_url", "model", "timeout"}},
	} {
		if i > 0 {
			out.WriteString("\n")
		}
		out.WriteString("[" + section.name + "]\n")
		for _, key := range section.keys {
			value, _ := json.Marshal(values[key])
			if key == "port" || key == "debug" {
				value = []byte(values[key])
			}
			out.WriteString(key + " = " + string(value) + "\n")
		}
	}
	return out.String(), nil
}
