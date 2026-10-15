package server

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Typed commands must land in the container log next to their answers:
// otherwise every reconnect replay restores output without the input.
func TestAppendConsoleEcho(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("CARDINAL_DATA_DIR", dir)

	appendConsoleEcho("srv-test-ctr", "tps\n")
	appendConsoleEcho("srv-test-ctr", "  \n")
	appendConsoleEcho("srv-test-ctr", "say hello\nworld")

	data, err := os.ReadFile(filepath.Join(dir, "logs", "srv-test-ctr.log"))
	if err != nil {
		t.Fatalf("log not written: %v", err)
	}
	got := string(data)
	for _, want := range []string{"> tps\n", "> say hello\n", "> world\n"} {
		if !strings.Contains(got, want) {
			t.Errorf("log missing %q, got:\n%s", want, got)
		}
	}
	if strings.Contains(got, "\n\n") {
		t.Errorf("blank lines leaked into log:\n%s", got)
	}
}

func TestAppendConsoleEchoRejectsBadIDs(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("CARDINAL_DATA_DIR", dir)

	appendConsoleEcho("../escape", "tps\n")
	appendConsoleEcho("", "tps\n")
	appendConsoleEcho("..", "tps\n")

	entries, err := os.ReadDir(filepath.Join(dir, "logs"))
	if err == nil && len(entries) > 0 {
		names := make([]string, 0, len(entries))
		for _, e := range entries {
			names = append(names, e.Name())
		}
		t.Fatalf("log written for invalid container id: %v", names)
	}
}

func TestSafeLogPartMirrorsCardinal(t *testing.T) {
	if got := safeLogPart("abc123"); got != "abc123" {
		t.Fatalf("plain id mangled: %q", got)
	}
	// Unsafe refs must not escape the logs dir: hashed fallback, 64 hex.
	got := safeLogPart("../../x")
	if len(got) != 64 {
		t.Fatalf("unsafe id not hashed: %q", got)
	}
	for _, c := range got {
		if !((c >= '0' && c <= '9') || (c >= 'a' && c <= 'f')) {
			t.Fatalf("non-hex fallback: %q", got)
		}
	}
}
