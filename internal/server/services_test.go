package server

import "testing"

func TestSplitServicePath(t *testing.T) {
	cases := []struct {
		path, action, name string
	}{
		{"/v1/services/my-svc", "", "my-svc"},
		{"/v1/services/my-svc/scale", "scale", "my-svc"},
		{"/v1/services/my-svc/remove", "remove", "my-svc"},
	}
	for _, c := range cases {
		action, name := splitServicePath(c.path)
		if action != c.action || name != c.name {
			t.Errorf("splitServicePath(%q) = (%q,%q), want (%q,%q)", c.path, action, name, c.action, c.name)
		}
	}
}

func TestSplitFnPath(t *testing.T) {
	cases := []struct {
		path, action, name string
	}{
		{"/v1/functions/hello", "", "hello"},
		{"/v1/functions/hello/invoke", "invoke", "hello"},
		{"/v1/functions/hello/remove", "remove", "hello"},
	}
	for _, c := range cases {
		action, name := splitFnPath(c.path)
		if action != c.action || name != c.name {
			t.Errorf("splitFnPath(%q) = (%q,%q), want (%q,%q)", c.path, action, name, c.action, c.name)
		}
	}
}
