package server

import "testing"

func TestNormalizePortMapping(t *testing.T) {
	cases := []struct {
		mapping string
		action  string
		want    string
	}{
		{"25566:25566/tcp", "ports/remove", "25566/tcp"},
		{"25566:25566", "ports/remove", "25566"},
		{"25566/tcp", "ports/remove", "25566/tcp"},
		{"25566", "ports/remove", "25566"},
		{"25566:25566/tcp", "ports/add", "25566:25566/tcp"},
		{"25566:25566", "ports/add", "25566:25566"},
		{"19132:19132/udp", "ports/remove", "19132/udp"},
	}
	for _, tc := range cases {
		if got := normalizePortMapping(tc.mapping, tc.action); got != tc.want {
			t.Errorf("normalizePortMapping(%q, %q) = %q, want %q", tc.mapping, tc.action, got, tc.want)
		}
	}
}
