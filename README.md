# SuperMonitor

SuperMonitor is an open-source, self-hosted control plane for monitoring AI and coding-agent quotas, credits, balances, refresh windows, and usage history.

> Current status: **v0.9.0 custom-address password login, resilient synchronization, and real usage history**. Connected accounts use provider-specific OAuth, credential-file, API-key, access-key, Cookie, or session-token flows; unsupported data is never replaced with demo quota.

## Principles

- Monitoring only: no API proxy, request routing, client account switching, or account rotation.
- Provider-native units remain provider-native.
- Every metric exposes freshness, source, and confidence.
- Secrets stay encrypted and excluded from source control.

## Live monitoring adapters

- Codex: import native `auth.json`, CPA/Sub2API-compatible JSON, or use OpenAI's official device-code flow, then read native quota windows.
- WorkBuddy / CodeBuddy CN: import a credential file or use official QR OAuth, then aggregate summary, paid-package, and free-package credits with real expiry times.
- WorkBuddy / CodeBuddy Global: import a credential file or use official QR OAuth, then read the international legacy billing endpoint.
- DeepSeek: enter an API key to read the official account balance.
- Xiaomi MiMo: paste the authenticated console Cookie to read Token Plan usage and reset time.
- TokenRhythm / 基元律动: paste a `sess_` browser session token or `tr_session` Cookie to read the CNY balance, expiry time, daily input/output/cache Token counters, request count, and official per-model aggregates from `tokenrhythm.studio`; it is a separate provider from Xiaomi MiMo.
- Zhipu: enter an API key to read native Coding Plan windows or the ordinary Open Platform CNY balance; a valid non-Coding-Plan key is no longer rejected as a missing subscription.
- Gemini CLI: import `oauth_creds.json` to read Code Assist model quota buckets. For unattended refresh after the access token expires, provide the deployment's own `SUPMON_GEMINI_OAUTH_CLIENT_ID` and `SUPMON_GEMINI_OAUTH_CLIENT_SECRET` environment variables.
- Claude Code: import `.credentials.json` to read five-hour, weekly, and model-specific OAuth usage windows.
- TRAE, Qoder CN/Global, Coze, Aliyun Bailian/BSS, Kiro, and Cursor use their dedicated verified connection and quota adapters.
- Store every credential with AES-GCM using a random key created in `SUPMON_DATA_DIR`.

## Alerts and verified activities

- Add multiple Feishu custom-bot or QQ Mail SMTP channels; Webhooks and SMTP authorization codes remain AES-GCM encrypted and are never returned to the browser.
- Notify once when a percentage-based quota reaches 15% or below, then re-arm after the quota recovers.
- Notify once within three days and once within one day of a provider-reported reset time.
- Refresh connected accounts on the server every 15 minutes with startup jitter and overlap protection, even when no browser is open. Refreshes use at most four provider calls in parallel, give each account an independent 45-second timeout, and use a configurable 10-minute whole-wave timeout (`SUPMON_SYNC_TIMEOUT`). Manual refresh postpones the next scheduled run instead of creating a burst.
- Scan notification state every ten minutes and re-scan verified activities after every account refresh. Automatically execute only activities backed by a verified adapter. The current automatic activity is WorkBuddy CN daily check-in; failures use backoff instead of retrying every poll.
- Delete an account and its credential, quota cache, active alerts, notification state, and delivery receipts together.

## Real usage history

- Persist usage by date, account, provider, and model; deleting an account also removes its locally recorded usage.
- Display Token trends for seven days, 30 days, one year, or all retained history.
- Build model share only from provider-returned model identifiers. Quota percentages, credits, and balances are never converted into fabricated Token or model data.
- TokenRhythm reloads the official rolling 30-day request history on every account refresh, idempotently repairs dates missed while SuperMonitor was offline, and then overwrites the current day with the fresher aggregate panel. Only counters, model, and date aggregates are stored; prompt and preview content are never persisted. Other cumulative provider sources can establish a baseline and record only positive deltas without double counting or negative values after a billing reset.
- Operation results use temporary accessible notifications for imports, OAuth connections, notification channels, refreshes, activity runs, ordering, and deletion. Success messages dismiss automatically instead of remaining on screen.

The background scheduler state is available from `GET /api/v1/sync/status`.

On Windows, outbound provider clients honor proxy environment variables first and then the current user's Internet Settings proxy. This keeps the service on the same route as the browser for region-sensitive OAuth endpoints.

Set `SUPMON_PROXY_URL` when the deployment needs an explicit HTTP(S) proxy. SuperMonitor writes scrubbed structured JSON logs to `SUPMON_DATA_DIR/supermonitor.log` while retaining console output; the file rotates at 10 MiB and keeps one backup. Set `SUPMON_LOG_FILE=-` only when Docker, systemd, or another supervisor already persists stdout.

Uploaded files are parsed in memory and are not retained as files. Tokens are never returned to the browser.

## Local development

Requirements: Go 1.25+, Node.js 24+, npm 11+.

```powershell
cd web
npm install
npm run api:generate
npm run dev
```

In another terminal:

```powershell
go run ./cmd/supermonitor
```

The Vite development server proxies `/api` to `http://127.0.0.1:8080`.

## Production build

```powershell
cd web
npm ci
npm run api:generate
npm run build
cd ..
go build -trimpath -ldflags "-s -w -X main.version=v0.9.0" -o bin/supermonitor-v0.9.0.exe ./cmd/supermonitor
```

Or use Docker Compose:

```powershell
$env:SUPMON_ADMIN_PASSWORD = Read-Host "SuperMonitor 管理密码（至少 12 个字符）" -MaskInput
docker compose up --build
```

The provided Compose file publishes only `127.0.0.1:8080`, but still requires `SUPMON_ADMIN_PASSWORD` because the application listens on the container network. Put the password in an uncommitted `.env` file or export it before running Compose. Set `SUPMON_ALLOW_INSECURE_REMOTE=true` only for a deliberately isolated deployment.

The image itself listens on `0.0.0.0:8080` inside the container. A direct `docker run` therefore refuses to start without administrator protection, even when Docker publishes the port only on loopback. The recommended direct-run setup is:

```powershell
docker build --build-arg VERSION=v0.9.0 -t supermonitor:v0.9.0 .
$password = Read-Host "SuperMonitor 管理密码（至少 12 个字符）" -MaskInput
docker run --rm --name supermonitor `
  -p 127.0.0.1:8080:8080 `
  -e "SUPMON_ADMIN_PASSWORD=$password" `
  -v supermonitor-data:/app/data `
  supermonitor:v0.9.0
```

For a deliberately isolated loopback-only container, `SUPMON_ALLOW_INSECURE_REMOTE=true` is an explicit alternative. Do not use that escape hatch when the published port, reverse proxy, or container network is reachable by untrusted clients.

## Security

Never commit `.env`, API keys, OAuth tokens, SQLite databases, backup bundles, logs, or exported authentication files. Use `.env.example` only as a field reference.

Local access remains passwordless when `SUPMON_ADMIN_PASSWORD` is empty. Non-loopback listeners require a management password of at least 12 Unicode characters unless `SUPMON_ALLOW_INSECURE_REMOTE=true` explicitly acknowledges an isolated outer security boundary. `SUPMON_ADMIN_TOKEN` remains a deprecated fallback for upgrades. Terminate HTTPS at the service or a trusted reverse proxy.

The login page follows the CPA connection pattern: enter the SuperMonitor address and management password. The current deployment address is filled automatically; entering another trusted SuperMonitor address submits directly to that instance and moves the browser there. The password is never placed in the URL or browser storage. It is exchanged for a random HttpOnly, SameSite session lasting 12 hours, or 30 days when “保持登录” is selected. API automation may alternatively send `Authorization: Bearer <password>`.

Example strong-password generation:

```powershell
[Convert]::ToHexString([Security.Cryptography.RandomNumberGenerator]::GetBytes(32)).ToLower()
```

When using a reverse proxy, preserve the public host and overwrite the forwarded scheme and host so same-origin checks and the Kiro OAuth callback use the external URL. For example, an Nginx location should include:

```nginx
proxy_set_header Host $http_host;
proxy_set_header X-Forwarded-Host $http_host;
proxy_set_header X-Forwarded-Proto $scheme;
```

Do not pass client-supplied forwarded headers through unchanged. Restrict direct access to the backend listener so only the trusted proxy can supply them.

## License

SuperMonitor is licensed under the GNU Affero General Public License v3.0. See [LICENSE](LICENSE).
