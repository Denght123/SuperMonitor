# SuperMonitor

SuperMonitor is an open-source, self-hosted control plane for monitoring AI and coding-agent quotas, credits, balances, refresh windows, and usage history.

> Current status: **v0.8 automatic synchronization, verified activity execution, and real usage history**. Connected accounts use provider-specific OAuth, credential-file, API-key, access-key, Cookie, or session-token flows; unsupported data is never replaced with demo quota.

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
- Refresh connected accounts on the server every 15 minutes with startup jitter, a bounded timeout, and overlap protection, even when no browser is open. Manual refresh postpones the next scheduled run instead of creating a burst.
- Scan notification state every ten minutes and re-scan verified activities after every account refresh. Automatically execute only activities backed by a verified adapter. The current automatic activity is WorkBuddy CN daily check-in; failures use backoff instead of retrying every poll.
- Delete an account and its credential, quota cache, active alerts, notification state, and delivery receipts together.

## Real usage history

- Persist usage by date, account, provider, and model; deleting an account also removes its locally recorded usage.
- Display Token trends for seven days, 30 days, one year, or all retained history.
- Build model share only from provider-returned model identifiers. Quota percentages, credits, and balances are never converted into fabricated Token or model data.
- TokenRhythm performs a one-time 30-day backfill from its official request-level usage list, stores only counters/model/date aggregates, and then refreshes the official current-day panel idempotently. Prompt and preview content are never requested for persistence. Other cumulative provider sources can establish a baseline and record only positive deltas without double counting or negative values after a billing reset.
- Operation results use temporary accessible notifications for imports, OAuth connections, notification channels, refreshes, activity runs, ordering, and deletion. Success messages dismiss automatically instead of remaining on screen.

The background scheduler state is available from `GET /api/v1/sync/status`.

On Windows, outbound provider clients honor proxy environment variables first and then the current user's Internet Settings proxy. This keeps the service on the same route as the browser for region-sensitive OAuth endpoints.

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
go build -o bin/supermonitor.exe ./cmd/supermonitor
```

Or use Docker Compose:

```powershell
docker compose up --build
```

## Security

Never commit `.env`, API keys, OAuth tokens, SQLite databases, backup bundles, logs, or exported authentication files. Use `.env.example` only as a field reference.

## License

SuperMonitor is licensed under the GNU Affero General Public License v3.0. See [LICENSE](LICENSE).
