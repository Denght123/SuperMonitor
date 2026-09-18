# Product

<!-- impeccable:product-schema 1 -->

## Platform

web

## Stack

Go backend with an embedded React + TypeScript frontend. SQLite is the default and only v1 database. Delivery targets are Docker/Compose and standalone Windows, macOS, and Linux binaries.

## Users

The primary user is a single self-hosting owner who manages multiple AI and coding-agent accounts across domestic and international providers. A typical deployment runs on a personal computer, NAS, VPS, or home server and must continue monitoring without an open browser.

## Product Purpose

SuperMonitor provides one trustworthy control plane for quota, credit, balance, reset-window, activity, and usage visibility across AI and agent platforms. Success means the owner can authenticate accounts once, see current quota state and refresh timing, inspect historical usage, run supported check-ins, and receive actionable warnings without proxying model traffic.

## Positioning

SuperMonitor unifies provider-native quota shapes without flattening unlike units into a misleading global score. Every metric carries its source, freshness, and confidence, and one identity can bind multiple related services without duplicating credentials.

## Operating Context

- Single-owner, self-hosted operation with up to roughly 20 accounts per provider.
- Browser-based OAuth, device-code authentication, API keys, and controlled credential imports.
- Long-running background polling, activity scheduling, webhook notifications, encrypted backup, and responsive browser access.
- Initial provider release: TRAE CN and International, Qoder CN and International, WorkBuddy/CodeBuddy CN and International, Codex, Gemini CLI/Code Assist, Claude Code, Alibaba Bailian Token Plan, TokenRhythm, DeepSeek, and Zhipu AI.
- Second provider release: Coze, Kiro, and Cursor.

## Capabilities and Constraints

- No API proxy, model request routing, client account switching, or automatic account rotation.
- Pure server deployment; no local helper and no automatic scanning of a user's computer.
- Official APIs are preferred. Undocumented community integrations require explicit opt-in, visible risk labeling, and a per-adapter kill switch.
- Credits, currency balances, token plans, and rate-limit windows retain their native units. Only comparable, trustworthy usage may be aggregated.
- Usage inferred from quota snapshots is labeled estimated and is never used to fabricate model distribution.
- Auto check-in is supported where available but is disabled by default.
- Built-in administrator authentication, optional TOTP, generic OIDC, and trusted reverse-proxy authentication are required.
- Credentials are encrypted using a deployment master key and never returned to the frontend.
- Simplified Chinese and English, system/light/dark themes, and full mobile operation are required.
- Raw quota snapshots retain 90 days, usage and activity detail retain 180 days, and daily aggregates retain indefinitely by default.
- Default alerts: warning at 20% remaining, critical at 10%, expiry/reset reminders within 24 hours, and alerts after two consecutive refresh failures or a failed check-in.

## Brand Commitments

- Product name: SuperMonitor.
- Operate-mode developer mission-control interface.
- Chart-first overview, compact account lists, and detail drawers.
- Visual references: workbuddy-switch for account readability and CLIProxyAPI for technical density, responsive transitions, and operational flow.
- Motion must communicate state, continuity, and feedback. Routine transitions stay fast, respect reduced-motion preferences, and do not delay work.

## Evidence on Hand

- Confirmed product brief and implementation decisions in the originating Codex conversation.
- Public reference projects: changexbc/workbuddy-switch, router-for-me/CLIProxyAPI, and provider-specific community adapters. Protocol behavior must be revalidated and licenses audited before code is reused.
- No production credentials, customer data, logos, or verified provider fixtures are currently available. Demonstration data must be labeled synthetic.

## Product Principles

1. Preserve provider truth instead of inventing a universal quota number.
2. Make freshness, provenance, confidence, and failure visible at the point of use.
3. Keep secrets local, encrypted, redacted, and excluded from source control.
4. Prefer calm operational speed over decorative interface motion.
5. Degrade honestly when a provider changes rather than showing stale success.

## Accessibility & Inclusion

Keyboard operation, semantic controls, readable status contrast, responsive layouts, and intentional `prefers-reduced-motion` behavior are required. Status must never be conveyed by color alone.
