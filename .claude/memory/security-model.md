---
name: security-model
description: Sonarr-style access control (one user, trusted networks, certificate validation) and the two rules that must never be relaxed
metadata:
  type: project
---

Redesigned on 2026-10-08 at the user's request, replacing the Tailscale-specific model (identity
allowlist, pending logins): "like Sonarr or Radarr: a user and password and a few settings".

- Settings (System → Security): authentication **required** or **not required on trusted
  networks** (default); trusted networks as CIDRs (default `127.0.0.0/8`, `::1/128`); certificate
  validation for outgoing HTTPS (**enabled** / **local_disabled** / **disabled**), applied through
  Go's default transport by `internal/adapters/outbound/tlspolicy`.
- One user (`Repository.FirstUser`); created only from a trusted network; requiring
  authentication needs the user (no lock-out); `-reset-auth` start-up flag to recover.
- Old saved settings (`localBypass`, `trustTailscale`…) are converted on read:
  `localBypass: false` → required.

**Never relax these rules** (`auth.Settings.Trusts`):
- Requests with proxy headers are not trusted (the peer is the proxy, not the caller).
- `Host` must be an address or `localhost`: a host name could be DNS rebinding (a malicious site
  pointing its name at this machine and reading data with the user's browser).

**Why:** without them, "no password on trusted networks" would let a website or a proxied remote
caller in. Status: built and tested against fakes and the test server; not yet confirmed in real use.

- **Access tokens (2026-10-12):** `gvt_` + 32 random bytes, only SHA-256 stored, shown once; `Authorization: Bearer gvt_…` reaches only `OwnershipService/CheckOwnership`; a token request ignores the session cookie and the trusted network; other `Authorization` headers are ignored (authenticating proxies forward their own); unknown tokens have their own throttle counter (10 failures in 15 minutes, then 429 with `Retry-After` 900 for unknown tokens only) so they cannot lock out password sign-ins; a valid token is looked up by hash first and always served, even while throttled (a sliding window would otherwise let one junk token every 90 s keep the owner refused); last use written at most once a minute; no cached validity, so revoking is immediate. A token never makes a request trusted.
