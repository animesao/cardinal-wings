package server

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestSFTPStoreRekeysByUsername(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("WINGS_DATA_DIR", dir)
	legacy := map[string]*sftpEntry{
		"srv-old-id": {Username: "srv-abc123", PasswordHash: "h", ContainerID: "srv-old-id"},
	}
	data, _ := json.Marshal(legacy)
	if err := os.WriteFile(filepath.Join(dir, "sftp-users.json"), data, 0600); err != nil {
		t.Fatal(err)
	}
	s, err := loadSFTPStore()
	if err != nil {
		t.Fatal(err)
	}
	e, ok := s.entryByUsername("srv-abc123")
	if !ok || e.ContainerID != "srv-old-id" {
		t.Fatalf("legacy owner credential lost: %+v %v", e, ok)
	}
}

func TestSFTPStoreMultiUser(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("WINGS_DATA_DIR", dir)
	s, err := loadSFTPStore()
	if err != nil {
		t.Fatal(err)
	}
	if err := s.set("srv-x", "srv-aaaa1111", "password-one"); err != nil {
		t.Fatal(err)
	}
	if err := s.set("srv-x", "srv-aaaa1111-bbbb2222", "password-two"); err != nil {
		t.Fatal(err)
	}
	if got := s.usernamesForContainer("srv-x"); len(got) != 2 {
		t.Fatalf("usernames = %v, want 2", got)
	}
	if _, ok := s.entryByUsername("srv-aaaa1111-bbbb2222"); !ok {
		t.Fatal("member entry missing")
	}
	// Removing one user keeps the other.
	if err := s.removeUser("srv-x", "srv-aaaa1111-bbbb2222"); err != nil {
		t.Fatal(err)
	}
	if _, ok := s.entryByUsername("srv-aaaa1111-bbbb2222"); ok {
		t.Fatal("removed member still present")
	}
	if _, ok := s.entryByUsername("srv-aaaa1111"); !ok {
		t.Fatal("owner entry wrongly removed")
	}
	// Removing the container drops everyone.
	if err := s.remove("srv-x"); err != nil {
		t.Fatal(err)
	}
	if got := s.usernamesForContainer("srv-x"); len(got) != 0 {
		t.Fatalf("usernames after remove = %v, want none", got)
	}
}

func TestSFTPStoreRelinkSurvivesRecreate(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("WINGS_DATA_DIR", dir)
	s, err := loadSFTPStore()
	if err != nil {
		t.Fatal(err)
	}
	if err := s.set("srv-old", "srv-aaaa1111", "pw"); err != nil {
		t.Fatal(err)
	}
	// Same username under a new container id (reinstall): overwrite, no dupes.
	if err := s.set("srv-new", "srv-aaaa1111", "pw2"); err != nil {
		t.Fatal(err)
	}
	if got := s.usernamesForContainer("srv-old"); len(got) != 0 {
		t.Fatalf("stale container entry = %v", got)
	}
	e, ok := s.entryByUsername("srv-aaaa1111")
	if !ok || e.ContainerID != "srv-new" {
		t.Fatalf("relinked = %+v %v", e, ok)
	}
}
