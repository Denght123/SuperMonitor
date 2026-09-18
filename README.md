# SuperMonitor

SuperMonitor is an open-source, self-hosted control plane for monitoring AI and coding-agent quotas, credits, balances, refresh windows, and usage history.

> Current status: **v0.2 live Codex adapter**. Codex supports encrypted OAuth credential import, official device-code login, token refresh, and live quota windows. Other provider cards are present but remain disabled until their authentication and quota protocols are verified.

## Principles

- Monitoring only: no API proxy, request routing, client account switching, or account rotation.
- Provider-native units remain provider-native.
- Every metric exposes freshness, source, and confidence.
- Secrets stay encrypted and excluded from source control.

## Live Codex monitoring

- Import a Codex `auth.json` or compatible CLIProxyAPI credential JSON.
- Sign in through OpenAI's official device-code page from a desktop or phone.
- Read native quota windows from `https://chatgpt.com/backend-api/wham/usage`.
- Label 5-hour, weekly, and monthly windows from the duration returned by OpenAI.
- Store credentials with AES-GCM using a random key created in `SUPMON_DATA_DIR`.

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
