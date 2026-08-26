// Package config loads /etc/cardinal-wings/config.toml: API keys with roles,
// bind address, TLS, and the credentials used to reach remote cluster nodes
// via their `cardinal serve` endpoints.
package config

import (
	"bufio"
	"crypto/subtle"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// Role enumerates the privilege levels an API key can have.
type Role string

const (
	RoleReadOnly Role = "readonly"
	RoleAdmin    Role = "admin"
)

// APIKey is a single credential the panel can authenticate with.
type APIKey struct {
	Name string `toml:"name"`
	Key  string `toml:"key"`
	Role Role   `toml:"role"`
}

// Node is a remote cluster node reachable through its `cardinal serve` API.
type Node struct {
	Name    string `toml:"name"`
	Address string `toml:"address"` // e.g. http://10.0.0.2:2375
	Token   string `toml:"token"`   // Bearer token of that node's serve API
	Enabled bool   `toml:"enabled"`
}

// Server holds the HTTP bind and TLS settings.
type Server struct {
	Host    string `toml:"host"`
	Port    int    `toml:"port"`
	TLSCert string `toml:"tls_cert"`
	TLSKey  string `toml:"tls_key"`
}

// Remote holds optional metrics settings.
type Remote struct {
	MetricsRequiresAuth bool `toml:"metrics_requires_auth"`
}

// Config is the root of the wings configuration.
type Config struct {
	Server Server   `toml:"server"`
	Keys   []APIKey `toml:"keys"`
	Nodes  []Node   `toml:"nodes"`
	Remote Remote   `toml:"remote"`
}

// Default returns a config that refuses to serve anything useful until edited.
func Default() *Config {
	return &Config{
		Server: Server{Host: "127.0.0.1", Port: 8080},
		Keys:   []APIKey{},
		Nodes:  []Node{},
		Remote: Remote{MetricsRequiresAuth: true},
	}
}

// Load reads a TOML config file (subset parser, no external deps). A missing
// file yields a default config with no keys so the daemon can start and warn.
func Load(path string) (*Config, error) {
	cfg := Default()
	f, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			return cfg, nil
		}
		return nil, fmt.Errorf("open %s: %w", path, err)
	}
	defer f.Close()

	section := ""
	scan := bufio.NewScanner(f)
	for scan.Scan() {
		line := strings.TrimSpace(scan.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		if strings.HasPrefix(line, "[") && strings.HasSuffix(line, "]") {
			section = parseSection(line)
			continue
		}
		key, value, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		key = strings.TrimSpace(key)
		value = strings.TrimSpace(value)
		switch section {
		case "server":
			switch key {
			case "host":
				cfg.Server.Host = unquote(value)
			case "port":
				if p, err := strconv.Atoi(value); err == nil {
					cfg.Server.Port = p
				}
			case "tls_cert":
				cfg.Server.TLSCert = unquote(value)
			case "tls_key":
				cfg.Server.TLSKey = unquote(value)
			}
		case "keys":
			cfg.consumeKey(key, value)
		case "nodes":
			cfg.consumeNode(key, value)
		case "remote":
			if key == "metrics_requires_auth" {
				cfg.Remote.MetricsRequiresAuth = value == "true"
			}
		}
	}
	if err := scan.Err(); err != nil {
		return nil, fmt.Errorf("read %s: %w", path, err)
	}
	return cfg, nil
}

func unquote(s string) string {
	return strings.Trim(s, `"'`)
}

// parseSection strips the outer braces from a TOML header, handling both a
// single table `[name]` and an array of tables `[[name]]`.
func parseSection(line string) string {
	line = strings.TrimSpace(line)
	if strings.HasPrefix(line, "[[") && strings.HasSuffix(line, "]]") {
		return strings.TrimSpace(line[2 : len(line)-2])
	}
	if strings.HasPrefix(line, "[") && strings.HasSuffix(line, "]") {
		return strings.TrimSpace(line[1 : len(line)-1])
	}
	return ""
}

// consumeKey accumulates consecutive [[keys]] entries sharing the "keys" table
// name. The tiny parser treats repeated `name`/`key`/`role` keys as one new
// entry each time `name` (whether spaced via comments) occurs.
func (c *Config) consumeKey(key, value string) {
	switch key {
	case "name":
		c.Keys = append(c.Keys, APIKey{Name: unquote(value)})
	default:
		if len(c.Keys) == 0 {
			return
		}
		last := &c.Keys[len(c.Keys)-1]
		switch key {
		case "key":
			last.Key = unquote(value)
		case "role":
			last.Role = Role(unquote(value))
		}
	}
}

func (c *Config) consumeNode(key, value string) {
	switch key {
	case "name":
		c.Nodes = append(c.Nodes, Node{Name: unquote(value)})
	default:
		if len(c.Nodes) == 0 {
			return
		}
		last := &c.Nodes[len(c.Nodes)-1]
		switch key {
		case "address":
			last.Address = unquote(value)
		case "token":
			last.Token = unquote(value)
		case "enabled":
			last.Enabled = value == "true"
		}
	}
}

// Validate checks the config is coherent enough to start.
func (c *Config) Validate() error {
	if c.Server.Port < 1 || c.Server.Port > 65535 {
		return fmt.Errorf("server.port %d out of range", c.Server.Port)
	}
	for _, k := range c.Keys {
		if k.Key == "" {
			return fmt.Errorf("key %q has an empty key value", k.Name)
		}
		if k.Role != RoleReadOnly && k.Role != RoleAdmin {
			return fmt.Errorf("key %q has invalid role %q (want readonly or admin)", k.Name, k.Role)
		}
	}
	if isExternal(c.Server.Host) {
		if len(c.Keys) == 0 {
			return fmt.Errorf("refusing to expose on %s with no API keys configured", c.Server.Host)
		}
		if (c.Server.TLSCert == "") != (c.Server.TLSKey == "") {
			return fmt.Errorf("TLS requires both tls_cert and tls_key")
		}
	}
	return nil
}

func isExternal(host string) bool {
	switch host {
	case "", "localhost", "127.0.0.1", "::1":
		return false
	}
	return true
}

// Authorize returns the role for the given bearer key, or false if unknown.
func (c *Config) Authorize(bearer string) (Role, bool) {
	for _, k := range c.Keys {
		if subtle.ConstantTimeCompare([]byte(bearer), []byte(k.Key)) == 1 {
			return k.Role, true
		}
	}
	return "", false
}

// AdminOnly reports whether a role may perform mutating operations.
func (r Role) AdminOnly() bool { return r == RoleAdmin }

// KeyName returns the name of the API key matching the bearer token, or false
// if the token is not a configured key.
func (c *Config) KeyName(bearer string) (string, bool) {
	for _, k := range c.Keys {
		if subtle.ConstantTimeCompare([]byte(bearer), []byte(k.Key)) == 1 {
			return k.Name, true
		}
	}
	return "", false
}

// WriteExample writes a documented empty config to path (used by install/docs).
func WriteExample(path string) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return err
	}
	return os.WriteFile(path, []byte(exampleTOML), 0600)
}

const exampleTOML = `# cardinal-wings configuration
#
# Copy to /etc/cardinal-wings/config.toml and set at least one API key.

[server]
host = "127.0.0.1"     # loopback by default; external binds need keys + TLS
port = 8080
# tls_cert = "/etc/cardinal-wings/server.crt"
# tls_key  = "/etc/cardinal-wings/server.key"

# Panel credentials. role is "readonly" or "admin".
[[keys]]
name = "main"
key = "change-me"
role = "admin"

# Remote cluster nodes, reached through their cardinal serve API.
[[nodes]]
name = "node-1"
address = "http://10.0.0.2:2375"
token = "that-node-serve-token"
enabled = false
`
