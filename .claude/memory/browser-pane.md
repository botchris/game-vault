---
name: browser-pane
description: The in-app browser caches index.html: bump ?v=N after rebuilding
metadata:
  type: feedback
---
- The in-app browser pane caches `index.html` aggressively: add `?v=N` to the URL after each
  rebuild. The SPA sends `no-cache` for index and `immutable` for `/assets/`.
