// Package config resolves independent CLI settings from TOML, environment and flags.
package config

import (
	"bytes"
	"errors"
	"fmt"
	"net/url"
	"os"
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
// An empty path selects ./jevwise.toml before the XDG/HOME global file.
// Only a missing default global file is optional; selected files never merge.
// Neither global Viper state nor the supplied flags are mutated.
func Load(path string, flags *pflag.FlagSet) (Config, error) {
	v := viper.New()
	defaults := map[string]string{"api_key": "", "base_url": "https://api.typesafe.ai", "model": "jev-latest", "provider": "jev", "timeout": "10s"}
	for key, value := range defaults {
		v.SetDefault(key, value)
	}
	path, required, err := ResolvePath(path)
	if err != nil {
		return Config{}, err
	}
	data, err := ReadFile(path)
	if err != nil && (required || !errors.Is(err, os.ErrNotExist)) {
		return Config{}, errors.New("cannot read configuration: file missing, unreadable or unsafe")
	}
	v.SetConfigType("toml")
	if err == nil && v.ReadConfig(bytes.NewReader(data)) != nil {
		return Config{}, errors.New("cannot read configuration: invalid TOML")
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
	c.Timeout, err = time.ParseDuration(values["timeout"])
	if err != nil {
		return Config{}, errors.New("timeout must be a positive duration")
	}
	if err := c.Validate(); err != nil {
		return Config{}, err
	}
	return c, nil
}

// Validate checks an already resolved configuration without reading files,
// flags or environment, and without changing its values. Credentials remain
// optional until service initialization. Failures never include input values.
func (c Config) Validate() error {
	if c.Timeout <= 0 {
		return errors.New("timeout must be a positive duration")
	}
	u, err := url.Parse(c.BaseURL)
	if err != nil || u.Hostname() == "" || (u.Scheme != "http" && u.Scheme != "https") || u.User != nil || u.RawQuery != "" || u.Fragment != "" {
		return errors.New("base_url must be an absolute HTTP(S) URL without credentials, query or fragment")
	}
	if strings.TrimSpace(c.Model) == "" {
		return errors.New("model must not be blank")
	}
	switch c.Provider {
	case "jev", "codex", "claude":
	default:
		return errors.New("provider must be jev, codex or claude")
	}
	return nil
}
