package agent

import (
	"context"
	"reflect"
	"strings"
	"testing"
)

func TestPullArgs(t *testing.T) {
	cases := []struct {
		ref, platform string
		want          []string
	}{
		{"nginx:latest", "", []string{"pull", "nginx:latest"}},
		{"alpine", "linux/arm64", []string{"pull", "alpine", "--platform", "linux/arm64"}},
	}
	for _, c := range cases {
		got := pullArgs(c.ref, c.platform)
		if !reflect.DeepEqual(got, c.want) {
			t.Errorf("pullArgs(%q,%q) = %v, want %v", c.ref, c.platform, got, c.want)
		}
	}
}

func TestBlueprintInstallArgs(t *testing.T) {
	got := blueprintInstallArgs("minecraft", "2g", "2", []string{"A=1", "B=2"}, true)
	want := []string{"blueprint", "install", "minecraft", "--memory", "2g", "--cpus", "2", "-e", "A=1", "-e", "B=2", "-y"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("blueprintInstallArgs = %v, want %v", got, want)
	}
}

func TestRunCardinalMissingBinary(t *testing.T) {
	// cardinal is not on PATH in test environments; running the command must
	// return an error rather than hang or panic.
	_, err := runCardinalOut(context.Background(), "definitely-not-a-command")
	if err == nil {
		t.Fatal("expected error when cardinal binary is missing")
	}
	if !strings.Contains(err.Error(), "cardinal") {
		t.Errorf("error should mention cardinal, got: %v", err)
	}
}
