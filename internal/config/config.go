// Package config resolves independent CLI settings from TOML, environment and flags.
package config

import (
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/spf13/pflag"
	"github.com/spf13/viper"
)

// Config contains runtime settings. Credentials are optional until a service call.
type Config struct {
	APIKey, BaseURL, Model, Provider string
	Timeout                          time.Duration
}

// Load applies changed flags > nonblank environment > TOML > defaults.
// An empty path discovers XDG_CONFIG_HOME/ts-jev/config.toml or
// HOME/.config/ts-jev/config.toml. Only a missing discovered file is optional.
// Neither global Viper state nor the supplied flags are mutated.
func Load(path string, flags *pflag.FlagSet) (Config, error) {
	v := viper.New()
	defaults := map[string]string{"api_key": "", "base_url": "https://api.typesafe.ai", "model": "jev-latest", "provider": "jev", "timeout": "10s"}
	for key, value := range defaults {
		v.SetDefault(key, value)
	}
	explicit := path != ""
	if !explicit {
		base := strings.TrimSpace(os.Getenv("XDG_CONFIG_HOME"))
		if base == "" {
			home, err := os.UserHomeDir()
			if err != nil {
				return Config{}, errors.New("cannot discover configuration: HOME is unset")
			}
			base = filepath.Join(home, ".config")
		}
		path = filepath.Join(base, "ts-jev", "config.toml")
	}
	v.SetConfigFile(path)
	v.SetConfigType("toml")
	if err := v.ReadInConfig(); err != nil && (explicit || !errors.Is(err, os.ErrNotExist)) {
		// Parser errors may contain file values, including credentials.
		return Config{}, errors.New("cannot read configuration: file missing, unreadable or invalid TOML")
	}
	upstream := map[string]string{"api_key": "TYPESAFE_API_KEY", "base_url": "TYPESAFE_BASE_URL", "model": "TYPESAFE_DEFAULT_MODEL"}
	env := make(map[string]any)
	for key := range defaults {
		for _, name := range []string{"TS_JEV_" + strings.ToUpper(key), upstream[key]} {
			if value := strings.TrimSpace(os.Getenv(name)); value != "" {
				env[key] = value
				break
			}
		}
		if flags != nil {
			if flag := flags.Lookup(strings.ReplaceAll(key, "_", "-")); flag != nil {
				if err := v.BindPFlag(key, flag); err != nil {
					return Config{}, fmt.Errorf("bind %s flag: %w", key, err)
				}
			}
		}
	}
	// Merge instead of Set so changed bound flags retain their higher priority.
	if err := v.MergeConfigMap(env); err != nil {
		return Config{}, errors.New("cannot apply environment configuration")
	}
	values := make(map[string]string)
	for key := range defaults {
		value, ok := v.Get(key).(string)
		if !ok {
			return Config{}, fmt.Errorf("%s must be a string", key)
		}
		values[key] = strings.TrimSpace(value)
	}
	c := Config{APIKey: values["api_key"], BaseURL: values["base_url"], Model: values["model"], Provider: values["provider"]}
	var err error
	c.Timeout, err = time.ParseDuration(values["timeout"])
	if err != nil || c.Timeout <= 0 {
		return Config{}, errors.New("timeout must be a positive duration")
	}
	u, err := url.Parse(c.BaseURL)
	if err != nil || u.Hostname() == "" || (u.Scheme != "http" && u.Scheme != "https") || u.User != nil || u.RawQuery != "" || u.Fragment != "" {
		return Config{}, errors.New("base_url must be an absolute HTTP(S) URL without credentials, query or fragment")
	}
	if c.Model == "" {
		return Config{}, errors.New("model must not be blank")
	}
	switch c.Provider {
	case "jev", "codex", "claude":
	default:
		return Config{}, errors.New("provider must be jev, codex or claude")
	}
	return c, nil
}
