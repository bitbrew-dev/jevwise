package config

import (
	"errors"
	"strings"
)

// Editor reads only the selected file's general.editor setting, not environment/SDK
// configuration. Invalid TOML falls back to vim and a flat file uses its editor key,
// so either can still be opened for repair or migration.
func Editor(path string) (string, error) {
	data, err := ReadFile(path)
	if err != nil {
		return "", errors.New("cannot read configuration to choose editor; use --editor to repair")
	}
	v, err := parseTOML(data)
	if err != nil {
		return "vim", nil
	}
	value := v.Get("general.editor")
	if isFlat(v) {
		value = v.Get("editor")
	}
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
