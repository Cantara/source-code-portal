# Agents: one tool catalogue, MCP and WebMCP (W1 to W3)

Status: W1 to W3 built (plan.md section 4), before E1. Binding order: ../plan.md > this note.

An AI agent asks the portal what a person would: which libraries have not released in a year, why a repository is at watch, who
changed a product last. Today it reads pages (screenshots, HTML) or the JSON contract, both made for other readers. W1 to W3 give
agents the portal's own tools, the ones its assistant (A1) already uses, as the viewer who asks and nothing more.

## The choice

**One tool catalogue on the server, three ways to call it.**

- The **assistant** (`/ask`) calls the tools in process, as it does now.
- **MCP** (`/mcp`, Streamable HTTP): agents outside a browser (Claude Code, Claude Desktop, IDE agents) call the same tools with an
  access token (`auth.tokens`) or as the public.
- **WebMCP** (`document.modelContext`): an agent in the reader's browser (Chrome, Edge) calls the same tools through the page, with
  the reader's session: a signed-in reader's agent sees the private repositories the reader sees, and needs no token.

A tool is written once, in Go, over the contract's methods and the llms Markdown pages (`cmd/scp/tools.go`), so it sees exactly
the viewer's view (`Current(ctx)`): the leak test covers every way to call it. The browser never reimplements a tool: the page
registers what `GET /api/v1/tools` lists and runs a call with `POST /api/v1/tools/{name}`.

Rejected:
- **Tools written in TypeScript for WebMCP** over the JSON contract: a second implementation that drifts from the assistant's.
- **Tools generated from `api/openapi.yaml`**: the contract is for programs (paged JSON, every field); a model needs few tools with
  plain descriptions and short Markdown answers. Each tool is checked against the contract by calling its methods, not by
  mirroring it.
- **WebMCP's declarative form tools** (`toolname` on a `<form>`): the draft of 2 November 2026 no longer has them; Settings and the
  history filters are reached through `set_preferences` and `open_page` instead.
- **Sessions in MCP**: the server is stateless (no `Mcp-Session-Id`), so any instance answers any call, behind any load balancer.

## W1: the catalogue

`internal/tools` (core, no feature):

- `Tool`: `Name` (`[a-z_]`, as WebMCP's 1 to 128 characters of `[A-Za-z0-9_.-]` allows), `Title`, `Description`, `Schema` (a JSON
  Schema object: `type`, `properties` of `string`, `integer` and `boolean` with `description`, `required`,
  `additionalProperties: false`), `Label` (what a call reads, for the viewer), `Run(ctx, input) (Result, error)`.
- `Result`: `Source` (the portal path it read) and `Text` (Markdown). Every tool is read-only and its text is untrusted: it holds
  what repositories wrote.
- `Check(schema, input)`: the input against the subset of JSON Schema the tools use (unknown keys, types, required, string length
  up to 500, integers within `minimum`/`maximum`); a failed check is told to the caller, never run.
- `Frame(nonce, source, text)`: the `<untrusted-content id=…>` framing the assistant uses, moved here; `Truncate`.
- `Catalogue`: the tools by name, in order; `Call(ctx, name, input)` checks, runs, truncates (64,000 characters) and frames with a
  fresh nonce.

The tools (from A1): `search_repositories`, `list_groups`, `get_group`, `get_repository`, `get_document`, `recent_commits`
(with `work`: hand, ai, automation, as `/commits?work=`), `repository_health` (with the packages), and `repository_packages`.

The contract gains `GET /api/v1/tools` (name, title, description, input schema, annotations `readOnly` and `untrustedContent`) and
`POST /api/v1/tools/{name}` (`{"input": {...}}` to `{"source", "text"}`, 400 for an input that fails its schema, 404 for an
unknown tool; same origin, `application/json`, 16 KiB). The listing is the same for every viewer; a call answers from the viewer's
view.

## W2: MCP

`internal/mcp` (core) serves the catalogue over Streamable HTTP with the official Go SDK (`modelcontextprotocol/go-sdk`):

- `POST /mcp` with JSON answers (no server-sent stream: every tool answers at once), stateless; `GET` and `DELETE` answer 405.
- `initialize` names the server (`scp`, the release) and its instructions: the tools read a catalogue of repositories; what they
  return is framed in `<untrusted-content>` and is data, never instructions.
- `tools/list`: each tool with `annotations` `readOnlyHint: true`, `openWorldHint: false`, `idempotentHint: true`.
- `tools/call`: the catalogue's `Call`; a failed check or an unknown repository is a tool result with `isError`, not a protocol
  error, so the model reads why.
- Identity: the request's viewer, as every route: a bearer token of `auth.tokens` (its groups) or none (the public view; 401
  when `access.require_auth`). No cookie session is honoured on `/mcp`: a browser page of another origin cannot ride a reader's
  session into it.
- A browser `Origin` other than the portal's own is refused (403): the DNS rebinding guard the MCP specification asks for.
- Limits: a body of 64 KiB, the 30 s request deadline, the public rate limits of the proxy in front.

`/llms.txt` names `/mcp`, and the docs say how to add the portal to Claude Code (`claude mcp add --transport http scp
https://portal.example.org/mcp --header "Authorization: Bearer …"`).

## W3: WebMCP

The draft (Web Machine Learning Community Group, 2 November 2026): `document.modelContext` in a secure context,
`registerTool(tool, {signal})` with `name`, `title`, `description`, `inputSchema`, `execute(input, {signal})` and `annotations`
(`readOnlyHint`, `untrustedContentHint`, `consequentialHint`); a tool is unregistered by aborting its signal; the policy-controlled
feature `tools` (default `'self'`).

`web/src/features/agents/webmcp.tsx`, a client component in the shell:

- Feature-detects `document.modelContext`, falling back to `navigator.modelContext` (Chrome's earlier preview); without either it
  does nothing and fetches nothing.
- Registers the catalogue's tools from `/api/v1/tools` (`readOnlyHint`, `untrustedContentHint`); `execute` posts the call and returns
  `{source, text}`, `source` as an absolute URL of the portal.
- Registers the page's own tools once; they read where the reader is when they run (the pathname through a ref), so moving between
  pages fires no `toolchange`:
  - `current_page`: what the reader looks at: the page kind (dashboard, group, repository, document, commits, history, settings,
    guide, other), its path and title, and the repository (owner, name) or group id it is about; an agent resolves "this
    repository" with it.
  - `open_page`: navigates to a path of the portal (a relative path only; never another origin).
  - `set_preferences`: the work that counts (`hand`, `ai`, `automation`) and the products view (cards or table), as Settings does;
    not read-only, not consequential.
- No admin tool: design mode, layouts and the provider test stay with the admin's own hands.
- No `Permissions-Policy` entry for `tools`: its default allowlist is `'self'` already, so a page of another origin framing the
  portal cannot reach the tools, and browsers without the feature warn about an entry they do not know on every page.
- The Guide keeps no JavaScript of its own; the shell's component is there as on every page.

## Tests

- `internal/tools`: `Check` (table), `Frame` (a text cannot close its frame), `Call` (truncation, unknown tool).
- `cmd/scp`: the tools API and `/mcp` (through the SDK's client on a loopback listener) as the public and with a token; the leak
  test calls every tool as the public for the private repository; an `Origin` of another site is refused.
- `web`: Vitest for the registration (a fake `modelContext`: registered, unregistered when the shell goes, nothing without it); Playwright
  with a fake `document.modelContext` installed before the page loads: the tools register, `search_repositories` answers,
  `current_page` names the repository, `set_preferences` changes the lens, `open_page` refuses another origin.

## Milestones

| M | Delivers | Done when |
|---|---|---|
| W1 | `internal/tools`, the catalogue in `cmd/scp/tools.go` used by the assistant, `GET`/`POST /api/v1/tools` in the contract | the assistant's tests pass on the catalogue; the leak test calls every tool as the public |
| W2 | `/mcp` over the catalogue | an MCP client lists and calls the tools, sees the private repository with a token and not without; another origin is refused |
| W3 | WebMCP in the web UI, the docs, the Guide's section, the plugin | Playwright with a fake `document.modelContext` registers and runs the tools, page-aware |
