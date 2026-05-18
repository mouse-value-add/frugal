# frugal

**Tool calls are the new tokens.**

For most agentic workloads the tool bill exceeds the model bill. Frugal is
an MCP server that routes every tool call your agent makes to the cheapest
provider that returns a result — free / local first, paid only as fallback,
premium only when you opt in.

Works with any model. One signed Go binary. Your keys. No account.
Source-available (BUSL 1.1 → Apache 2.0).

[frugal.sh](https://frugal.sh) · [Strategy](./STRATEGY.md)

## Install

```bash
curl -fsSL https://frugal.sh/install | bash
frugal mcp install
```

The first command drops the binary in your `$PATH`. The second auto-detects
Claude Desktop, Cursor, and Claude Code and merges `frugal` into each
configured MCP server list.

## Set your keys

BYOK. Frugal reads provider credentials from your environment:

```bash
export SERPER_API_KEY=...   # routed search
export TAVILY_API_KEY=...   # routed search (cheaper paid alternative)
```

That's it. Restart your agent. `frugal__search` shows up in the tool
picker.

## The rack-rate gap

Tool prices haven't fallen the way model prices have. Tavily at $0.008/call
is 8× Serper at $0.001/call. SearXNG, running on your own machine, is free.

| Capability | Free / local | Cheap paid | Premium paid |
|---|---|---|---|
| Search | SearXNG (self-host) | **Serper** $0.001/call | You.com $0.005 · Exa $0.007 · Tavily $0.008 |
| Extract | Trafilatura, readability.js, Mercury | — | Firecrawl $0.001–0.005/page |
| Browse | local Playwright + Chromium | Browserless ~$0.002/30s | Browserbase $0.10/hr |
| Code exec | local Docker | E2B ~$0.10/hr (2 vCPU) | Modal |
| Embeddings | nomic-embed-text, bge-large | text-embedding-3-small $0.02/1M tok | 3-large, Voyage-3, Cohere |
| Transcription | whisper.cpp | Deepgram Nova $0.0043/min | OpenAI Whisper $0.006/min |

Frugal walks the columns left to right. Each tool call goes to the leftmost
configured provider that clears the recipe's quality bar; you keep the gap.

## What ships today

Phase 1 v1.0 — one MCP server, one tool, two providers:

- `frugal__search` — routed search across **Serper** (`$0.001/call`) and
  **Tavily** (`$0.008/call`).
- Stdio + Streamable HTTP transports.
- `frugal mcp install` writes the right config into Claude Desktop,
  Cursor, and Claude Code.

## What's coming

- **Phase 2** — `frugal__extract` and `frugal__browse` with $0 / local
  providers (SearXNG, Playwright, Trafilatura) and the free/local-first
  routing rule that uses them.
- **Phase 3** — embeddings, transcription, code execution, local chat
  models, semantic cache.
- **Phase 4** — Frugal Cloud: hosted MCP endpoint for users who don't want
  to operate the local stack themselves.

Roadmap and rationale in [STRATEGY.md](./STRATEGY.md).

## From source

```bash
git clone https://github.com/brainsparker/frugal.git && cd frugal && make build
```

## License

[BUSL 1.1](./LICENSE) — self-hosting and internal commercial use are
permitted. Each release converts to Apache 2.0 four years after publication.
Plain-English summary in [LICENSE-BUSL-FAQ.md](./LICENSE-BUSL-FAQ.md).

## Security

Private vulnerability reports via [GitHub Security
Advisories](https://github.com/brainsparker/frugal/security/advisories/new).
Full policy in [SECURITY.md](./SECURITY.md).
