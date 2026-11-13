package config

import (
	"errors"
	"net/netip"
	"os"
	"strconv"
	"strings"

	"github.com/spf13/viper"
)

// Default MCP listen settings. The server binds only literal loopback IPs.
const (
	DefaultMCPHost = "127.0.0.1"
	DefaultMCPPort = 8080
)

var errHost = errors.New("mcp.host must be a literal loopback IP without scheme or port, such as 127.0.0.1")
var errMCPPort = errors.New("mcp.port must be an integer from 1 to 65535")

// MCPAddress resolves [mcp] host and port (nonblank TS_JEV_MCP_BASE_URL and
// TS_JEV_MCP_PORT > TOML > defaults) into a canonical loopback IP:port. It reads no other settings, so only a
// missing file falls back to defaults; unsafe files, invalid TOML or values fail.
func MCPAddress(path string) (string, error) {
	resolved, _, err := ResolvePath(path)
	if err != nil {
		return "", err
	}
	data, err := ReadFile(resolved)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return "", errors.New("cannot read configuration: file unreadable or unsafe")
	}
	v := viper.New()
	if err == nil {
		if v, err = readSettings(data); err != nil {
			return "", err
		}
	}
	values, err := mcpValues(v, true)
	if err != nil {
		return "", err
	}
	return mcpAddress(values["host"], values["port"])
}

// mcpEnvNames maps [mcp] keys to their environment overrides, unchanged since the flat layout.
var mcpEnvNames = map[string]string{"host": "TS_JEV_MCP_BASE_URL", "port": "TS_JEV_MCP_PORT"}

// mcpValues reads both keys, accepting a TOML integer or string port, and
// validates the result. Environment overrides apply only when env is true.
func mcpValues(v *viper.Viper, env bool) (map[string]string, error) {
	values := map[string]string{"host": DefaultMCPHost, "port": strconv.Itoa(DefaultMCPPort)}
	for key := range values {
		switch raw := v.Get("mcp." + key).(type) {
		case nil:
		case string:
			values[key] = strings.TrimSpace(raw)
		case int64:
			if key != "port" {
				return nil, errHost
			}
			values[key] = strconv.FormatInt(raw, 10)
		case int:
			if key != "port" {
				return nil, errHost
			}
			values[key] = strconv.Itoa(raw)
		default:
			if key == "port" {
				return nil, errMCPPort
			}
			return nil, errHost
		}
		if env {
			if value := strings.TrimSpace(os.Getenv(mcpEnvNames[key])); value != "" {
				values[key] = value
			}
		}
	}
	if _, err := mcpAddress(values["host"], values["port"]); err != nil {
		return nil, err
	}
	return values, nil
}

func mcpAddress(host, port string) (string, error) {
	addr, err := netip.ParseAddr(host)
	if err != nil || !addr.IsLoopback() || addr.Zone() != "" {
		return "", errHost
	}
	n, err := strconv.ParseUint(port, 10, 16)
	if err != nil || n == 0 {
		return "", errMCPPort
	}
	return netip.AddrPortFrom(addr.Unmap(), uint16(n)).String(), nil
}
