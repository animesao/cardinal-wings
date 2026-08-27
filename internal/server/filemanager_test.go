package server

import (
	"encoding/base64"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"strings"
	"testing"
)

// TestFmListScriptGenerated verifies the generated shell script has exactly one
// fmt.Sprintf placeholder (the path) so `%` in the embedded commands survive.
func TestFmListScriptGenerated(t *testing.T) {
	got := fmt.Sprintf(fmListScript, "/home/container")
	// After substitution there should be no leftover `%` verb placeholders.
	if strings.Contains(got, "%%") {
		t.Fatalf("escaped verbs left in generated script: %s", got)
	}
	if !strings.Contains(got, "P=/home/container") {
		t.Fatalf("path not substituted: %s", got)
	}
}

// TestFmListScriptParsing runs the actual script through `sh` against a temp
// directory and parses the output the way handleFmList does.
func TestFmListScriptParsing(t *testing.T) {
	sh, err := exec.LookPath("sh")
	if err != nil {
		t.Skip("sh not available")
	}

	dir := t.TempDir()
	if err := os.WriteFile(dir+"/hello world.txt", []byte("abc"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(dir+"/sub", 0755); err != nil {
		t.Fatal(err)
	}

	script := fmt.Sprintf(fmListScript, shq(dir))
	out, err := exec.Command(sh, "-c", script).Output()
	if err != nil {
		t.Fatalf("script failed: %v\nscript:\n%s", err, script)
	}

	var fileFound, dirFound bool
	for _, line := range strings.Split(strings.TrimRight(string(out), "\n"), "\n") {
		if line == "" {
			continue
		}
		parts := strings.Split(line, "\t")
		if len(parts) < 3 {
			t.Fatalf("malformed line: %q", line)
		}
		if parts[0] == "f" && parts[1] == "3" {
			fileFound = true
		}
		if parts[0] == "d" && len(parts) >= 4 {
			if nameBytes, err := base64.StdEncoding.DecodeString(parts[3]); err == nil && string(nameBytes) == "sub" {
				dirFound = true
			}
		}
	}
	if !fileFound || !dirFound {
		t.Fatalf("expected file (size 3) and sub dir in:\n%s", out)
	}
}

// TestFmActionRouting guards against the path-parsing regression where
// /fm/list dispatched with the container id as the action ("unknown fm
// action: <id>") instead of reaching handleFmList. When fm/list is handled,
// it attempts a container exec and fails upstream (no runtime here) with a
// 502; a routing bug fails with a 404 "unknown fm action".
func TestFmActionRouting(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/v1/containers/deadbeef/fm/list?path=cm9vdA==", nil)
	rr := httptest.NewRecorder()
	handleContainerRef(rr, req)
	if rr.Code == http.StatusNotFound && strings.Contains(rr.Body.String(), "unknown fm action") {
		t.Fatalf("fm/list misrouted: %s", rr.Body.String())
	}
	// It should attempt the exec and fail upstream (502) or succeed with a real
	// runtime. A 404 not_found here would mean the container may not exist.
	if rr.Code == http.StatusNotFound {
		return
	}
	if rr.Code != http.StatusGatewayTimeout && rr.Code != http.StatusBadGateway && rr.Code != http.StatusOK {
		t.Errorf("fm/list status = %d, want 200/502, got body: %s", rr.Code, rr.Body.String())
	}
}
