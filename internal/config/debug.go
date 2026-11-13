package config

import (
	"errors"
	"os"
	"strconv"
	"strings"

	"github.com/spf13/viper"
)

var errDebug = errors.New("general.debug must be true or false")

// Debug resolves general.debug (nonblank TS_JEV_DEBUG > TOML > false). It reads
// no other settings; only a missing file falls back to false.
func Debug(path string) (bool, error) {
	resolved, _, err := ResolvePath(path)
	if err != nil {
		return false, err
	}
	data, err := ReadFile(resolved)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return false, errors.New("cannot read configuration: file unreadable or unsafe")
	}
	v := viper.New()
	if err == nil {
		if v, err = readSettings(data); err != nil {
			return false, err
		}
	}
	enabled, err := debugValue(v)
	if err != nil {
		return false, err
	}
	if value := strings.TrimSpace(os.Getenv("TS_JEV_DEBUG")); value != "" {
		if enabled, err = strconv.ParseBool(value); err != nil {
			return false, errDebug
		}
	}
	return enabled, nil
}

func debugValue(v *viper.Viper) (bool, error) {
	switch raw := v.Get("general.debug").(type) {
	case nil:
		return false, nil
	case bool:
		return raw, nil
	case string:
		enabled, err := strconv.ParseBool(strings.TrimSpace(raw))
		if err != nil {
			return false, errDebug
		}
		return enabled, nil
	}
	return false, errDebug
}
