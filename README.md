# SuperMonitor

SuperMonitor is an open-source, self-hosted control plane for monitoring AI and coding-agent quotas, credits, balances, refresh windows, and usage history.

> Current status: **v0.4 provider protocol overhaul**. Codex, WorkBuddy / CodeBuddy CN and Global, DeepSeek, and Xiaomi MiMo have built-in live adapters. Every catalog platform can also connect through a verified custom quota endpoint mapping; an account is saved only after the endpoint authenticates and returns a valid live value.

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
- TRAE, Qoder, Coze, Bailian, Zhipu, Gemini, Claude Code, Kiro, and Cursor: configure the platform's real quota URL, authentication header/Cookie, and JSON field paths. SuperMonitor validates the live response before persisting the account.
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
