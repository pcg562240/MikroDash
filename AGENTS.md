# MikroDash Chinese fork

- Work on `zh-CN`; keep official business sources (`web`, `cmd`, `internal`, `third_party`, Go module files) identical to the commit in `localization/upstream.json`.
- Implement Chinese presentation through `localization/` and isolated build output. Preserve field values, identifiers, event names, permission checks, escaping, and router-owned content.
- Never add real router/NAS configurations, credentials, `.env`, `.secret`, `/data`, private keys or personal backups to Git. Use synthetic fixtures only.
- Do not deploy to NAS or change RouterOS/OpenClash settings without a separate explicit user request. Do not reuse production writable data volumes for tests.
- New untranslated UI words and moved adapter anchors require review, not suppression of tests. Keep technical-name exceptions explicit.
- Run the checks in `docs/zh-CN/MAINTAINING.md`. Never claim coverage of every runtime message from the static inventory alone.
- Upstream sync opens a review PR. No automatic merging, image deployment or force-pushing over the Chinese branch.
- Preserve MIT license and upstream attribution. Do not present this community fork as official.
