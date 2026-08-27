package config

import (
	"os"
	"path/filepath"
	"testing"
)

func writeTemp(t *testing.T, content string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "config.toml")
	if err := os.WriteFile(p, []byte(content), 0600); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestLoadMissingFileReturnsDefault(t *testing.T) {
	cfg, err := Load(filepath.Join(t.TempDir(), "nope.toml"))
	if err != nil {
		t.Fatalf("expected nil error, got %v", err)
	}
	if cfg.Server.Port != 8080 || cfg.Server.Host != "127.0.0.1" {
		t.Fatalf("unexpected defaults: %+v", cfg.Server)
	}
	if len(cfg.Keys) != 0 {
		t.Fatalf("expected no keys, got %d", len(cfg.Keys))
	}
}

func TestLoadParsesKeysAndNodes(t *testing.T) {
	content := `
[server]
host = "0.0.0.0"
port = 9000
tls_cert = "/c.pem"
tls_key = "/k.pem"

[[keys]]
name = "main"
key = "secret-a"
role = "admin"

[[keys]]
name = "viewer"
key = "secret-b"
role = "readonly"

[[nodes]]
name = "node-1"
address = "http://10.0.0.2:2375"
token = "tok"
enabled = true
`
	cfg, err := Load(writeTemp(t, content))
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Server.Port != 9000 || cfg.Server.Host != "0.0.0.0" {
		t.Fatalf("server parse wrong: %+v", cfg.Server)
	}
	if len(cfg.Keys) != 2 {
		t.Fatalf("want 2 keys, got %d", len(cfg.Keys))
	}
	if cfg.Keys[0].Role != RoleAdmin || cfg.Keys[1].Role != RoleReadOnly {
		t.Fatalf("roles wrong: %+v", cfg.Keys)
	}
	if len(cfg.Nodes) != 1 || cfg.Nodes[0].Name != "node-1" || !cfg.Nodes[0].Enabled {
		t.Fatalf("nodes wrong: %+v", cfg.Nodes)
	}
}

func TestLoadStripsInlineComments(t *testing.T) {
	// Mirrors the config written by install.sh and config.example.toml, where the
	// host line carries an inline comment. The hand-rolled parser must not leak
	// the comment into the parsed host value (which would crash the listen).
	content := `
[server]
host = "0.0.0.0"     # 0.0.0.0 = remote panel
port = 8080

[[keys]]
name = "panel"   # panel key
key = "f887c2d33815"
role = "admin"   # full access
`
	cfg, err := Load(writeTemp(t, content))
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Server.Host != "0.0.0.0" {
		t.Fatalf("host leaked inline comment: got %q", cfg.Server.Host)
	}
	if cfg.Server.Port != 8080 {
		t.Fatalf("port wrong: %d", cfg.Server.Port)
	}
	if len(cfg.Keys) != 1 || cfg.Keys[0].Key != "f887c2d33815" || cfg.Keys[0].Name != "panel" {
		t.Fatalf("keys wrong with inline comments: %+v", cfg.Keys)
	}
	if cfg.Keys[0].Role != RoleAdmin {
		t.Fatalf("role leaked comment: got %q", cfg.Keys[0].Role)
	}
}

func TestValidateRejectsExternalWithoutKeys(t *testing.T) {
	cfg := Default()
	cfg.Server.Host = "0.0.0.0"
	if err := cfg.Validate(); err == nil {
		t.Fatal("expected error for external bind with no keys")
	}
}

func TestValidateRejectsPartialTLS(t *testing.T) {
	cfg := Default()
	cfg.Server.Host = "0.0.0.0"
	cfg.Keys = []APIKey{{Name: "k", Key: "x", Role: RoleAdmin}}
	cfg.Server.TLSCert = "/c.pem" // no key
	if err := cfg.Validate(); err == nil {
		t.Fatal("expected error for partial TLS")
	}
}

func TestAuthorizeConstantTimeLookup(t *testing.T) {
	cfg := Default()
	cfg.Keys = []APIKey{
		{Name: "main", Key: "abc", Role: RoleAdmin},
		{Name: "viewer", Key: "xyz", Role: RoleReadOnly},
	}
	if role, ok := cfg.Authorize("abc"); !ok || role != RoleAdmin {
		t.Fatalf("admin key failed: %v %v", role, ok)
	}
	if role, ok := cfg.Authorize("xyz"); !ok || role != RoleReadOnly {
		t.Fatalf("readonly key failed: %v %v", role, ok)
	}
	if _, ok := cfg.Authorize("nope"); ok {
		t.Fatal("unknown key should not authorize")
	}
}

func TestWriteExample(t *testing.T) {
	p := filepath.Join(t.TempDir(), "config.example.toml")
	if err := WriteExample(p); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(p); err != nil {
		t.Fatalf("example not written: %v", err)
	}
}
