---
name: telegram
description: Telegram Bot API as the notifications connector: URL shape, methods used, limits, errors and what the bogus-token probes returned (2026-10-15)
metadata:
  type: reference
---
As of 2026-10-15 (https://core.telegram.org/bots/api), used by `internal/adapters/outbound/telegram`:
- Every call is `POST https://api.telegram.org/bot<token>/<method>`: the token is in the URL path, so
  an error from the transport names it. Everything the adapter returns passes through `redact`
  (raw and path-escaped token replaced by `…`); a test asserts no error holds the token.
- Methods: `sendMessage` (JSON: `chat_id`, `text`, `parse_mode` HTML, `link_preview_options.is_disabled`),
  `sendPhoto` (multipart, `caption`), `sendMediaGroup` (multipart, `media` JSON list, `attach://name` for
  uploaded bytes, the caption on the first item), `getUpdates` (POST JSON with `allowed_updates`
  `message` and `channel_post`, the chat finder: last update's `chat.id`).
- Limits: text 4096 characters, caption 1024 (after the markup is read), at most 10 per album (we send 2);
  about one message a second to a chat, so the pace is 3 s per connection.
- Answers are `{"ok":bool,"result":…,"error_code","description","parameters":{"retry_after"}}`.
  401 and 404 = bad token, 400 "chat not found" = wrong chat, 403 = the bot was blocked or removed,
  409 on `getUpdates` = a webhook is set on the bot, 429 = flood limit with `retry_after`, 5xx = outage.
  A 400 about the image ("wrong file identifier", "failed to get HTTP URL content", "wrong type of the
  web page content") is not about the chat: the message goes again as text.
- Probe with a made-up token (bogus credentials only):
  `POST /bot123456:NOT-A-REAL-TOKEN/getMe` → HTTP 401 `{"ok":false,"error_code":401,"description":"Unauthorized"}`;
  `POST /bot123456:NOT-A-REAL-TOKEN/getUpdates` (JSON `{"allowed_updates":["message"]}`) → the same HTTP 401.
- Never send to a real chat in tests: the fake server reproduces these answers.
