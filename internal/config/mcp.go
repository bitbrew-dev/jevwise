package config

import (
	"bytes"
	"errors"
	"net/netip"
	"os"
	"strconv"
	"strings"

	"github.com/spf13/viper"
)

// Default MCP listen settings. The server binds only literal loopback IPs.
const (
	DefaultMCPBaseURL = "127.0.0.1"
	DefaultMCPPort    = 8080
)

var errMCPPort = errors.New("mcp_port must be an integer from 1 to 65535")

// MCPAddress resolves mcp_base_url and mcp_port (nonblank environment > TOML >
// defaults) into a canonical loopback IP:port. It reads no other settings, so only a
// missing file falls back to defaults; unsafe files, invalid TOML or values fail.
func MCPAddress(path string) (string, error) {
	v := viper.New()
	v.SetConfigType("toml")
	resolved, _, err := ResolvePath(path)
	if err != nil {
		return "", err
	}
	data, err := ReadFile(resolved)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return "", errors.New("cannot read configuration: file unreadable or unsafe")
	}
	if err == nil && v.ReadConfig(bytes.NewReader(data)) != nil {
		return "", errors.New("cannot read configuration: invalid TOML")
	}
	values, err := mcpValues(v, true)
	if err != nil {
		return "", err
	}
	return mcpAddress(values["mcp_base_url"], values["mcp_port"])
}

// mcpValues reads both keys, accepting a TOML integer or string port, and
// validates the result. Environment overrides apply only when env is true.
func mcpValues(v *viper.Viper, env bool) (map[string]string, error) {
	values := map[string]string{"mcp_base_url": DefaultMCPBaseURL, "mcp_port": strconv.Itoa(DefaultMCPPort)}
	for key := range values {
		switch raw := v.Get(key).(type) {
		case nil:
		case string:
			values[key] = strings.TrimSpace(raw)
		case int64:
			if key != "mcp_port" {
				return nil, errors.New("mcp_base_url must be a string")
			}
			values[key] = strconv.FormatInt(raw, 10)
		case int:
			if key != "mcp_port" {
				return nil, errors.New("mcp_base_url must be a string")
			}
			values[key] = strconv.Itoa(raw)
		default:
			if key == "mcp_port" {
				return nil, errMCPPort
			}
			return nil, errors.New("mcp_base_url must be a string")
		}
		if env {
			if value := strings.TrimSpace(os.Getenv("TS_JEV_" + strings.ToUpper(key))); value != "" {
				values[key] = value
			}
		}
	}
	if _, err := mcpAddress(values["mcp_base_url"], values["mcp_port"]); err != nil {
		return nil, err
	}
	return values, nil
}

func mcpAddress(host, port string) (string, error) {
	addr, err := netip.ParseAddr(host)
	if err != nil || !addr.IsLoopback() || addr.Zone() != "" {
		return "", errors.New("mcp_base_url must be a literal loopback IP without scheme or port, such as 127.0.0.1")
	}
	n, err := strconv.ParseUint(port, 10, 16)
	if err != nil || n == 0 {
		return "", errMCPPort
	}
	return netip.AddrPortFrom(addr.Unmap(), uint16(n)).String(), nil
}
