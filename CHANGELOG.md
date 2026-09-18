# Changelog

All notable changes to SuperMonitor are documented here.

## [0.4.0] - 2026-09-18

### Added

- Windows system-proxy discovery for all provider clients, fixing Codex device login when the browser uses a local proxy.
- CPA, Sub2API, nested, array, camelCase, and refresh-token-only Codex credential imports.
- WorkBuddy CN summary/paid/free resource aggregation with per-package balances and real expiry handling.
- Verified custom quota endpoint connections for every remaining catalog platform; accounts are saved only after a live response is authenticated and mapped.

### Changed

- Rebuilt quota and account views into CPA-style provider groups with three-to-four compact cards per row.
- Reduced typography and visual density, normalized control alignment, and moved to a quiet light gray palette.
- Replaced every pending provider card with an actionable connection form while preserving honest live-data validation.

## [0.3.0] - 2026-09-18

### Added

- Real WorkBuddy / CodeBuddy CN and Global credential import, official QR OAuth, refresh-token renewal, and billing credit retrieval.
- Real DeepSeek API-key balance retrieval through the official `/user/balance` endpoint.
- Real Xiaomi MiMo Token Plan retrieval from a locally encrypted console Cookie.
- Verified provider logos for every catalog platform through the Lobe Icons brand set.
- Provider-specific connection forms for OAuth files, QR login, API keys, and Cookies.

### Changed

- Removed legacy production demo records and replaced chart/account placeholders with honest empty states.
- Reduced dashboard typography and card density to a compact CLIProxyAPI-style monitoring layout.
- Generalized account summaries, quota windows, and refresh handling across live providers.

## [0.2.0] - 2026-09-18

### Added

- Live Codex account pools with OAuth credential import and official OpenAI device-code login.
- Native Codex quota windows, exact reset times, credits, manual refresh, and automatic token refresh.
- AES-GCM credential vault backed by a random local key under `SUPMON_DATA_DIR`.
- Provider-specific connection catalog covering the 15 requested domestic and international platforms.
- Colored quota progress bars and responsive Codex connection flows for desktop and mobile.

### Changed

- Increased the interface typography for long monitoring sessions.
- Made the light theme the default while preserving dark and system preferences.
- Reworked account onboarding so each provider has an independent entry point and supported authentication methods.

### Security

- Uploaded OAuth files are parsed in memory and are never retained as files.
- Access and refresh tokens never return to the browser.
- Common OAuth export filenames, credential stores, databases, logs, and reference repositories are excluded from source control.

### Known limitations

- Codex is the only live provider adapter in this release; the remaining provider cards are explicitly marked as pending.
- Device-code login requires outbound access to `auth.openai.com`, and live quota refresh requires access to `chatgpt.com`.

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
