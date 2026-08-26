# Security Policy

cardinal-wings is a remote management API — a compromised instance can control
containers on every connected node, so we take reports seriously.

## Reporting a vulnerability

**Please do not open a public issue for security problems.**

- Email the maintainer directly (see git history / maintainer profile on
  GitHub) or open a **private** security advisory via
  GitHub → Security → Report a vulnerability.
- Include: affected version, steps to reproduce, impact, and a suggested fix
  if you have one.
- We aim to acknowledge within 48 hours and to ship a fix (and, where
  warranted, a release) promptly.

## Supported versions

| Version | Supported          |
| ------- | ------------------ |
| latest release | ✅ |
| older releases | ❌ — upgrade |

## Security-relevant behavior

- **Auth:** Bearer tokens with roles (`readonly`/`admin`); constant-time
  comparison. Admin-only mutations are enforced in handlers and audited.
- **Rate limiting:** per-IP and per-key token buckets, configurable.
- **Bind default:** loopback only. External binds require keys and TLS.
- **TLS:** supported; `WINGS_TLS=1` in install.sh generates a self-signed
  cert for testing (use a real cert in production).
- **Audit log:** every mutating request is recorded to `wings-audit.jsonl`
  (rotated at 10 MiB).
- **Node tokens:** remote `cardinal serve` tokens live only in
  `/etc/cardinal-wings/config.toml` and are never echoed by the API.

## Reporting a cardinal vulnerability

Issues in cardinal itself (the runtime wings manages) belong to the cardinal
repository's security policy, not here.
