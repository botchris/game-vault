---
name: ntfy
description: ntfy as the notifications connector: publish headers, image upload, errors and the health probe (2026-10-15); never publish to a public topic while testing
metadata:
  type: reference
---
As of 2026-10-15 (https://docs.ntfy.sh/publish/), used by `internal/adapters/outbound/ntfy`:
- Publish is `PUT <server>/<topic>` (server default `https://ntfy.sh`, any self-hosted one works).
  Text messages: the text is the body. Headers: `Title`, `Priority` (`default`, `high` for attention
  and security events), `Click` (the link), `Attach` (a public image URL), `Message` and `Filename`
  (when the body is an uploaded image: the text then travels in the `Message` header, newlines as `\n`).
- Headers are ASCII: non-ASCII text goes RFC 2047 encoded; the `Message` header is cut below ~3900
  bytes because ntfy and proxies refuse headers of about 4 KB.
- Optional access token in `Authorization: Bearer <token>` only, never in the URL or an error.
- Errors are JSON with an `error` text: 401 and 403 = access denied (bad token, or it may not publish
  to the topic), 404 = unknown address, 413 = the image is too large (the message goes as text),
  429 = rate limited with `Retry-After`, 5xx = outage.
- Public topics on ntfy.sh are readable by anyone who guesses the name: the setup help says to pick a
  name nobody would guess. Never publish to one while probing.
- Probe (the only one allowed against the real service): `GET https://ntfy.sh/v1/health` → HTTP 200
  `{"healthy":true}` (2026-10-15).
