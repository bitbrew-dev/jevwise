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
