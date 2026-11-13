package config

import (
	"bytes"
	"errors"

	"github.com/spf13/viper"
)

var errInvalidTOML = errors.New("cannot read configuration: invalid TOML")

// parseTOML parses only the selected bytes into a fresh Viper instance. It never
// uses Viper search paths, ReadInConfig, environment binding or defaults.
func parseTOML(data []byte) (*viper.Viper, error) {
	v := viper.New()
	v.SetConfigType("toml")
	if err := v.ReadConfig(bytes.NewReader(data)); err != nil {
		return nil, errInvalidTOML
	}
	return v, nil
}

// ErrFlatLayout reports a pre-grouped configuration file. It never includes values.
var ErrFlatLayout = errors.New("configuration uses the old flat layout; run `jevwise config migrate` to convert it to [general], [mcp] and [provider.jev] tables")

// flatKeys are the top-level settings of the flat layout used before grouped tables.
var flatKeys = []string{"api_key", "base_url", "model", "provider", "timeout", "editor", "mcp_base_url", "mcp_port", "debug"}

// isFlat reports whether any flat-layout key appears at the top level. A
// top-level provider table belongs to the grouped layout; a provider value does not.
func isFlat(v *viper.Viper) bool {
	for _, key := range flatKeys {
		if !v.InConfig(key) {
			continue
		}
		if _, table := v.Get(key).(map[string]any); key == "provider" && table {
			continue
		}
		return true
	}
	return false
}

// readSettings parses grouped configuration and rejects the flat layout.
func readSettings(data []byte) (*viper.Viper, error) {
	v, err := parseTOML(data)
	if err != nil {
		return nil, err
	}
	if isFlat(v) {
		return nil, ErrFlatLayout
	}
	return v, nil
}
