---
name: local-images
description: Images are downloaded into config/game-data; the browser only loads remote images through the allowlisted proxy
metadata:
  type: project
---
- Covers and sheet images are downloaded into `config/game-data/<title> [<id>]/` (Plex-style),
  with `assets.json` recording where each came from.
- The browser loads remote images only through `/media/proxy`, for hosts a provider declares
  with `ImageHoster` (some CDNs, like TheGamesDB's, reject hotlinks).
