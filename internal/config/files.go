package config

import (
	"bytes"
	"context"
	"crypto/rand"
	_ "embed"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/viper"
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
	v := viper.New()
	v.SetConfigType("toml")
	if err := v.ReadConfig(bytes.NewReader(data)); err != nil {
		return nil, errors.New("cannot read configuration: invalid TOML")
	}
	defaults := defaultValues()
	defaults["editor"] = "vim"
	values := make(map[string]string)
	for key, value := range defaults {
		v.SetDefault(key, value)
		value, ok := v.Get(key).(string)
		if !ok {
			return nil, errors.New("configuration settings must be strings")
		}
		values[key] = strings.TrimSpace(value)
	}
	return values, nil
}

// View shows known file values/defaults, never environment overrides, arbitrary
// extra keys or comments. Only api_key is redacted; URL userinfo/query/fragment
// are refused before display. Other settings must not contain credentials.
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
	for _, key := range []string{"api_key", "base_url", "model", "provider", "timeout", "editor"} {
		value, _ := json.Marshal(values[key])
		out.WriteString(key + " = " + string(value) + "\n")
	}
	return out.String(), nil
}
