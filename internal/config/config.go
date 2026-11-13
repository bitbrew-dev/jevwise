// Package config resolves independent CLI settings from TOML, environment and flags.
package config

import (
	"errors"
	"fmt"
	"net/url"
	"os"
	"slices"
	"strings"
	"time"

	"github.com/bitbrew-dev/jevwise/internal/gemini"
	"github.com/spf13/pflag"
	"github.com/spf13/viper"
)

// Config contains runtime settings. Credentials are optional until a service call.
type Config struct {
	APIKey, BaseURL, Model, Provider string
	Timeout                          time.Duration
}

// Providers lists the accepted provider names; each has a [provider.<name>] table.
var Providers = []string{"jev", "gemini", "codex", "claude"}

var errProvider = errors.New("provider must be jev, gemini, codex or claude")

// Load applies changed flags > nonblank environment > TOML > defaults.
// An empty path selects ./.jevwise.toml, then the Jevwise global file.
// Only a missing default global file is optional; selected files never merge.
// Provider settings come from [provider.<general.provider>]; flat files fail.
// TOML API keys expand $NAME/${NAME}; environment and flag keys stay literal.
// Neither global Viper state nor the supplied flags are mutated.
func Load(path string, flags *pflag.FlagSet) (Config, error) {
	path, required, err := ResolvePath(path)
	if err != nil {
		return Config{}, err
	}
	data, err := ReadFile(path)
	if err != nil && (required || !errors.Is(err, os.ErrNotExist)) {
		return Config{}, errors.New("cannot read configuration: file missing, unreadable or unsafe")
	}
	v := viper.New()
	if err == nil {
		if v, err = readSettings(data); err != nil {
			return Config{}, err
		}
	}
	values := make(map[string]string)
	provider, _, err := resolveSetting(v, flags, "", "provider", "general.provider")
	if err != nil {
		return Config{}, err
	}
	if !slices.Contains(Providers, provider) {
		return Config{}, errProvider
	}
	values["provider"] = provider
	for _, key := range []string{"api_key", "base_url", "model", "timeout"} {
		value, fromFile, err := resolveSetting(v, flags, provider, key, "provider."+provider+"."+key)
		if err != nil {
			return Config{}, err
		}
		if key == "api_key" && fromFile {
			value = strings.TrimSpace(os.ExpandEnv(value))
		}
		values[key] = value
	}
	return fromValues(values)
}

// resolveSetting applies changed flag > nonblank environment > TOML path > default
// for one setting of the given provider and reports whether it came from the file.
func resolveSetting(v *viper.Viper, flags *pflag.FlagSet, provider, key, path string) (string, bool, error) {
	if flags != nil {
		if flag := flags.Lookup(strings.ReplaceAll(key, "_", "-")); flag != nil && flag.Changed {
			return strings.TrimSpace(flag.Value.String()), false, nil
		}
	}
	for _, name := range envNames(provider, key) {
		if value := strings.TrimSpace(os.Getenv(name)); value != "" {
			return value, false, nil
		}
	}
	if value, ok, err := fileString(v, path); ok || err != nil {
		return value, ok, err
	}
	return defaultValues(provider)[key], false, nil
}

// fileString reads one string setting from the parsed file only.
func fileString(v *viper.Viper, path string) (string, bool, error) {
	raw := v.Get(path)
	if raw == nil {
		return "", false, nil
	}
	value, ok := raw.(string)
	if !ok {
		return "", false, fmt.Errorf("%s must be a string", path)
	}
	return strings.TrimSpace(value), true, nil
}

// envNames keeps credentials and destinations per provider: Jev names never
// configure Gemini, and Gemini names follow google-genai (GOOGLE_API_KEY wins).
func envNames(provider, key string) []string {
	if provider == "gemini" {
		switch key {
		case "api_key":
			return []string{"GOOGLE_API_KEY", "GEMINI_API_KEY"}
		case "base_url":
			return []string{"GOOGLE_GEMINI_BASE_URL"}
		case "model":
			return nil
		}
	}
	switch key {
	case "api_key":
		return []string{"JEV_API_KEY", "JEVWISE_API_KEY", "TS_JEV_API_KEY", "TYPESAFE_API_KEY"}
	case "base_url":
		return []string{"TS_JEV_BASE_URL", "TYPESAFE_BASE_URL"}
	case "model":
		return []string{"TS_JEV_MODEL", "TYPESAFE_DEFAULT_MODEL"}
	}
	return []string{"TS_JEV_" + strings.ToUpper(key)}
}

func defaultValues(provider string) map[string]string {
	values := map[string]string{"api_key": "", "base_url": "https://api.typesafe.ai", "model": "jev-latest", "provider": "jev", "timeout": "10s"}
	if provider == "gemini" {
		values["base_url"], values["model"] = gemini.DefaultBaseURL, gemini.DefaultModel
	}
	return values
}

func fromValues(values map[string]string) (Config, error) {
	c := Config{APIKey: values["api_key"], BaseURL: values["base_url"], Model: values["model"], Provider: values["provider"]}
	var err error
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
	if !slices.Contains(Providers, c.Provider) {
		return errProvider
	}
	return nil
}
