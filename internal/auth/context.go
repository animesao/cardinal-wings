package auth

import (
	"context"
	"net/http"

	"github.com/animesao/cardinal-wings/internal/config"
)

// withRole stores the authenticated role on a request context.
func withRole(r *http.Request, role config.Role) context.Context {
	return WithRole(r.Context(), role)
}

// WithRole stores the authenticated role.
func WithRole(ctx context.Context, role config.Role) context.Context {
	return context.WithValue(ctx, ctxRole, role)
}

// RoleFrom reads the role stored by Authenticate.
func RoleFrom(ctx context.Context) (config.Role, bool) {
	role, ok := ctx.Value(ctxRole).(config.Role)
	return role, ok
}

// WithKeyName stores the authenticated key's name on the context.
func WithKeyName(ctx context.Context, name string) context.Context {
	return context.WithValue(ctx, ctxKeyName, name)
}

// KeyNameFrom reads the key name stored by Authenticate.
func KeyNameFrom(ctx context.Context) (string, bool) {
	name, ok := ctx.Value(ctxKeyName).(string)
	return name, ok
}
