# Optional traffic extension invariants

- Keep this module separate from the Chinese translation catalog and official business sources.
- Never use production data directories or credentials in tests. Fixtures bind loopback only.
- Keep all controller operations read-only and fixed-path. Do not expose a generic controller proxy.
- Enforce MikroDash session validation and per-router read/history/manage permissions on the server, not only the UI.
- Never return stored secrets or put them in URLs/logs. Credentials belong only in encrypted extension configuration.
- Never turn a reset, first baseline, reconnect, missing counter or unknown device into an invented usage amount.
- Do not sum infrastructure/transit traffic into terminal totals or claim proxy-only device attribution.
- Run Go race tests, JavaScript tests, adapter typecheck, official source baseline check and responsive UI QA.
- Source implementation does not authorize GHCR publication or NAS/ROS/OpenClash deployment. Obtain a separate explicit request for production changes.
