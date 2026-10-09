package config

import (
	"bytes"
	"errors"
	"strings"

	"github.com/spf13/viper"
)

// Editor reads only the selected file's editor setting, not environment/SDK
// configuration. Invalid TOML falls back to vim so the file can be repaired.
func Editor(path string) (string, error) {
	data, err := ReadFile(path)
	if err != nil {
		return "", errors.New("cannot read configuration to choose editor; use --editor to repair")
	}
	v := viper.New()
	v.SetConfigType("toml")
	if err := v.ReadConfig(bytes.NewReader(data)); err != nil {
		return "vim", nil
	}
	value := v.Get("editor")
	if value == nil {
		return "vim", nil
	}
	editor, ok := value.(string)
	if !ok {
		return "", errors.New("editor must be one executable name or path; use --editor to repair")
	}
	if strings.TrimSpace(editor) == "" {
		return "vim", nil
	}
	return strings.TrimSpace(editor), nil
}
