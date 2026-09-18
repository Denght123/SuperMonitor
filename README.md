# SuperMonitor

SuperMonitor is an open-source, self-hosted control plane for monitoring AI and coding-agent quotas, credits, balances, refresh windows, and usage history.

> Current status: **v0.5 native provider connections**. Codex, WorkBuddy / CodeBuddy CN and Global, DeepSeek, Xiaomi MiMo, TokenRhythm, Zhipu, Gemini CLI, and Claude Code have dedicated live adapters. Platforms whose native protocol is not yet verified remain visibly disabled instead of exposing a generic field-mapping form.

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
- TokenRhythm / 基元律动: paste a `sess_` browser session token or `tr_session` Cookie to read the CNY balance and expiry time from `tokenrhythm.studio`; it is a separate provider from Xiaomi MiMo.
- Zhipu: enter an API key to read native five-hour and weekly Coding Plan windows, preserving credits versus percentage semantics.
- Gemini CLI: import `oauth_creds.json` to read Code Assist model quota buckets. For unattended refresh after the access token expires, provide the deployment's own `SUPMON_GEMINI_OAUTH_CLIENT_ID` and `SUPMON_GEMINI_OAUTH_CLIENT_SECRET` environment variables.
- Claude Code: import `.credentials.json` to read five-hour, weekly, and model-specific OAuth usage windows.
- TRAE, Qoder, Coze, Bailian, Kiro, and Cursor remain disabled until their dedicated authentication and quota contracts are verified.
- Store every credential with AES-GCM using a random key created in `SUPMON_DATA_DIR`.

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
