# Changelog

All notable changes to SuperMonitor are documented here.

## [0.1.0] - 2026-09-18

### Added

- Go service foundation with Chi, SQLite WAL mode, schema migrations, health endpoints, refresh orchestration, and an SSE event stream.
- React and TypeScript operator cockpit embedded into the production Go binary.
- Synthetic overview, account pool, native quota signal, alert, activity, and usage datasets.
- Thirty-day token trend and model-distribution dashboards.
- Responsive desktop rail, compact rail, and mobile bottom navigation.
- Dark, light, and system theme states with reduced-motion support.
- Account detail drawer, command search, global refresh feedback, loading, error, and disabled states.
- OpenAPI contract with generated TypeScript types.
- Docker, Compose, CI, secret scanning, and strict secret/runtime-data exclusions.
- Product and design-system documentation under `PRODUCT.md` and `DESIGN.md`.

### Security

- No real provider credentials are read or stored in this release.
- Environment files, credential exports, databases, backups, logs, and private keys are excluded from source control.

### Known limitations

- Provider adapters, OAuth/device-code flows, encryption-backed credential storage, check-in execution, notifications, and English localization are scheduled for later versions.
- All visible dashboard data is synthetic and labeled as such.
