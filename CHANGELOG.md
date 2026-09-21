# Changelog

All notable changes to SuperMonitor are documented here.

## [0.9.0] - 2026-09-21

### Added

- CPA-style connection login with an editable SuperMonitor address and a user-defined management password.
- Safe cross-instance navigation: the browser posts the password directly to the selected instance, never places it in a URL, and never saves it to local or session storage.
- Optional 30-day HttpOnly browser sessions for trusted personal devices, with explicit logout and the existing login-failure throttling.
- `SUPMON_ADMIN_PASSWORD` as the primary deployment setting, with backward-compatible support for `SUPMON_ADMIN_TOKEN` during upgrades.

### Changed

- Administrator authentication copy, API payloads, Docker configuration, and deployment guidance now consistently use management-password terminology.
- Browser sessions use SameSite=Lax so a successful top-level connection from another trusted SuperMonitor address can complete reliably; state-changing requests still require an exact same-origin check.

## [0.8.1] - 2026-09-21

### Added

- Optional administrator-token protection with an HttpOnly browser session and Bearer-token support for API automation.
- Safe non-loopback startup defaults: remote listeners require an administrator token of at least 24 Unicode characters unless an explicitly isolated deployment opts out.
- Persistent scrubbed structured logs at `SUPMON_DATA_DIR/supermonitor.log`, 10 MiB rotation with one retained backup, protected data-file collision checks, and an explicit stdout-only mode for supervised deployments.
- SSE heartbeat frames and integration coverage proving updates still arrive after the HTTP server write timeout.

### Changed

- Account refresh now uses at most four workers, an independent 45-second timeout per account, a configurable 10-minute whole-wave timeout, stable bounded error summaries, and parent cancellation.
- TokenRhythm reloads and idempotently stores its rolling 30-day official history on every refresh, repairing gaps created while the service was offline.
- Provider `lastCheckedAt` now advances after a successful real account refresh.
- Provider badges cover every current adapter, OAuth polling no longer recreates its interval for each pending response, and settings copy now describes the implemented notification controls.
- `.env.example` now documents the real proxy, log, and administrator-token settings and no longer advertises an unused master-key variable.
- Direct Docker runs now document their required remote-listener protection, while the loopback-only Compose mapping retains an explicit isolated-boundary opt-in; reverse-proxy guidance preserves the public Host and forwarded scheme/host needed by OAuth and same-origin checks.

### Fixed

- Long-lived SSE clients no longer lose account and refresh events after 30 seconds.
- A zero or negative Zhipu CNY balance is now critical instead of healthy.
- Unknown API routes and unsupported methods now return the same structured JSON error shape as known endpoints.
- Removed the unused always-zero KPI delta field instead of implying an unavailable period comparison.
- Administrator-session states now provide complete recovery feedback, and app panels narrower than 320 px no longer create root-level horizontal scrolling.

## [0.8.0] - 2026-09-20

### Added

- Browser-independent account synchronization every 15 minutes, with randomized startup delay, a three-minute run timeout, serialized execution, manual-refresh deferral, and `GET /api/v1/sync/status` diagnostics.
- Automatic verified-activity discovery after every account refresh; supported activities run automatically, while unknown platforms remain absent instead of receiving speculative entries.
- A unified accessible Toast system for account imports, OAuth connections, notification-channel setup and tests, refreshes, activity execution, provider ordering, and account deletion. Success notifications dismiss automatically and can be closed manually.
- User-controlled provider ordering shared by the overview and account pool, persisted in local browser storage with accessible move controls.
- Account/provider/model-attributed usage storage with idempotent absolute daily updates, cumulative snapshot baselines, positive-delta recording, counter-reset handling, and account-deletion cascade.
- TokenRhythm's official 30-day history backfill and daily usage panel, including input/output/cache Tokens, request counts, and real per-model aggregates without retaining prompt or preview content.

### Changed

- Token and request KPIs, daily trends, and model share now read only from verified persisted usage. Credits, balances, rate-window percentages, and unattributed Token totals cannot create a fake model distribution.
- Refresh-all now updates account quotas and then probes the verified activity registry through the same serialized server-side coordinator.
- The previous permanent server-event banner was replaced by temporary operation feedback; background SSE updates remain quiet and refresh the displayed data in place.
- WorkBuddy, Codex, balance providers, and Token plans continue to retain their native units while usage history remains a separate measurement path.

### Fixed

- Account-deletion success messages no longer remain visible after navigation.
- The model chart no longer claims a hard-coded 30-day range when it represents retained, provider-attributed history.
- Repeated usage snapshots and repeated official daily reads no longer double count Token totals.

## [0.7.0] - 2026-09-20

### Added

- Per-account deletion with explicit confirmation; the account, encrypted credential, cached quota windows, notification state, delivery receipts, and related active alerts are removed together.
- Encrypted Feishu bot and QQ Mail SMTP notification channels with masked targets, test delivery, channel removal, and a manual evaluation view.
- Persistent low-quota alerts at 15% or below, three-day and one-day reset reminders, per-channel delivery deduplication, recovery handling, and retry-safe scheduling.
- Ten-minute discovery and automatic execution for the currently verified WorkBuddy CN daily check-in adapter, with Asia/Shanghai day boundaries and failure backoff.
- Token trend ranges for 7 days, 30 days, one year, and all retained history.

### Changed

- Zhipu API-key handling now distinguishes Coding Plan quotas from ordinary Open Platform billing, reads the official available CNY balance when supported, and falls back to verified model access without reporting a false missing-plan error.
- Notification and account-management settings now include complete loading, error, confirmation, disabled, empty, and responsive states in the existing compact visual system.
- Activity status failures are now shown on their affected account card instead of being mistaken for an empty activity list.
- Alert state is reconciled against the provider's current quota windows, so expired or removed windows no longer leave stale alerts behind.

### Security

- Feishu Webhooks and SMTP authorization codes are encrypted with the local AES-GCM vault and are never returned by the API.
- Feishu delivery accepts only official HTTPS hosts and port 443, refuses redirects and ambiguous responses, and removes Webhook tokens from network errors and logs.
- WorkBuddy automatic check-in requires an explicit boolean activity-status field; missing or malformed status data can no longer trigger a speculative check-in.
- Account deletion is serialized with refresh and activity writes, and notification events plus alerts are stored atomically, preventing deleted accounts or orphan alerts from reappearing during concurrent work.

## [0.6.2] - 2026-09-19

### Changed

- Rebuilt quota presentation around one account card per identity, matching CPA-style account separation while preserving the existing SuperMonitor visual language.
- Codex cards now show account email, normalized plan name, five-hour quota, weekly quota, reset times, and authentication method together.
- WorkBuddy cards now show only the official account-level credit total in overview and account-pool surfaces; package-level grants move into a compact expandable detail list.

### Fixed

- Added exact-response deduplication for WorkBuddy Billing resources without collapsing legitimate grants that share a package code but have different amounts or expiry times.
- Clarified that small WorkBuddy activity grants are included in the displayed total and are not counted again when their detail list is expanded.

## [0.6.1] - 2026-09-19

### Added

- Real WorkBuddy CN daily check-in discovery and execution; the activity center only lists activities confirmed by the platform API.
- Zhipu Open Platform API-key validation through the official model-list endpoint when an account has no Coding Plan.

### Fixed

- Codex CPA imports now explain rotated or reused refresh tokens and reuse newer locally stored credentials for the same account when available.
- Codex accounts without credits no longer render a misleading `0 credits` card.
- Rotated Codex credentials are persisted when a refresh succeeds before a subsequent quota request fails.
- Codex and WorkBuddy activity/billing types are displayed independently according to their real charging model.

## [0.5.0] - 2026-09-19

### Added

- Dedicated Zhipu API-key adapter for native five-hour and weekly quota windows.
- Dedicated Claude Code and Gemini CLI OAuth credential-file imports.
- Dedicated TokenRhythm browser-session adapter for CNY balance, usage, and expiry fields.
- Official TokenRhythm brand mark and strict separation from Xiaomi MiMo.

### Fixed

- WorkBuddy cards now display real remaining credits such as `2100 credits`; percentages are used only to fill progress bars.
- Balance providers display CNY amounts, while Codex-style rate windows retain percentage values.
- Codex CPA/Sub2API imports select one coherent account record instead of mixing sibling tokens and metadata.
- Codex authorization errors no longer trap the connection dialog; close, backdrop, and Escape always work.

### Changed

- Replaced the generic endpoint and JSON-path form with dedicated OAuth, file, API-key, Cookie, or session-token onboarding.
- Unverified providers are clearly marked as being researched and cannot create misleading pseudo-connections.

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
