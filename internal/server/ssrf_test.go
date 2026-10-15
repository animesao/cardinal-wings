package server

import (
	"context"
	"strings"
	"testing"
)

func TestValidateFetchURLRejectsDangerous(t *testing.T) {
	for _, raw := range []string{
		"",
		"ftp://example.com/x",
		"file:///etc/passwd",
		"gopher://example.com/",
		"http://user:pass@example.com/",
		"http://127.0.0.1:2375/containers/json",
		"http://localhost:8080/v1/version",
		"http://[::1]/",
		"http://0.0.0.0/",
		"http://169.254.169.254/latest/meta-data/",
		"http://224.0.0.1/",
		"http://10.0.0.1:evil/",
		"not a url",
	} {
		if _, err := validateFetchURL(raw); err == nil {
			t.Errorf("expected %q to be rejected", raw)
		}
	}
}

func TestValidateFetchURLAllowsPublicAndLAN(t *testing.T) {
	// example.com resolves to public IPs; 192.168.x.x stays allowed for
	// node-to-node transfers over private networks.
	for _, raw := range []string{
		"https://example.com/mods/mod.jar",
		"http://192.168.1.20:8080/v1/containers/x/backup",
		"http://10.0.0.5/file.tar.gz",
	} {
		u, err := validateFetchURL(raw)
		if err != nil {
			t.Errorf("expected %q to be accepted: %v", raw, err)
		} else if u.Scheme != "http" && u.Scheme != "https" {
			t.Errorf("scheme lost for %q", raw)
		}
	}
}

func TestFetchGetRefusesMetadata(t *testing.T) {
	// Even with an admin key, the cloud metadata address must not be
	// fetchable through pull/restore.
	if _, err := fetchGet(context.Background(), "http://169.254.169.254/latest/meta-data/"); err == nil {
		t.Fatal("expected metadata IP to be rejected")
	} else if !strings.Contains(err.Error(), "forbidden") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestValidatePanelURL(t *testing.T) {
	for _, raw := range []string{"https://panel.example.com", "http://192.168.0.5:3000"} {
		if err := validatePanelURL(raw); err != nil {
			t.Errorf("expected %q to be accepted: %v", raw, err)
		}
	}
	for _, raw := range []string{"", "ftp://x/", "http://user:pw@host/", "://bad", "https://"} {
		if err := validatePanelURL(raw); err == nil {
			t.Errorf("expected %q to be rejected", raw)
		}
	}
}
