# Sign-in and admin
Status: S1, in progress (plan.md section 4). Binding order: ../plan.md > this note. Adapted from krantho's admin authentication (OIDC
with PKCE, a shared-token login, its own session) to SCP's model: no user store, SQLite a rebuildable cache, several instances.

## Scope
- SCP signs people in itself, through any OpenID Connect provider (Zitadel, Keycloak, Entra ID, Google, GitHub through Dex), as an
  alternative to the trusted authenticating proxy (access.md), which stays.
- Bearer tokens for machines: scripts and LLM agents read the API, Markdown and `/llms.txt` with the access the token grants.
- An admin role: admins see private repositories, the operator status and the assistant's provider (assistant.md) in the web UI.
- Off by default: without `auth.session_key_file` nothing here is registered and SCP behaves as before.

## Principal
Every request has one viewer, decided once by the middleware (first match wins):
1. The web UI server's API call: `X-Scp-Token` proves it, `X-Scp-Viewer` carries the page's viewer (http-api.md).
2. `Authorization: Bearer <token>`: a configured token (`auth.tokens[]`).
3. The session cookie `scp_session`.
4. The trusted proxy's user and groups headers (access.md).

A viewer is `{user, name, groups, via}`; from it and the config follow `private` (a group in `access.private_groups`, `"*"` for every
signed-in viewer) and `admin` (`auth.admin_emails` or a group in `auth.admin_groups`; an admin is also private). The roles are derived
on every request from the config, so a change to the lists applies at once. A wrong bearer token answers 401, never anonymous.

## Sessions
- No server state: the cookie is the session, sealed with AES-256-GCM under a key derived by HKDF-SHA256 from `auth.session_key_file`
  (at least 32 bytes; label `scp session v1`), the cookie name as additional data. It holds `{iss, sub, email, name, groups, iat, exp}`.
- `auth.session_ttl` (default 12 h): past it the viewer signs in again (at the provider, silently while its own session lasts). A
  session older than half its lifetime is resealed on a page request, so an active reader stays signed in.
- `HttpOnly`, `Path=/`, `SameSite=Lax`, `Secure` when `server.public_url` is https.
- Every instance of a cluster reads the same key file, so a session works on each. Revocation is a key rotation (everyone signs in
  again); a person removed at the provider loses access when the session ends.

## OIDC
- `coreos/go-oidc` v3 (discovery, JWKS, ID token verification) and `golang.org/x/oauth2` (code exchange, PKCE S256).
- `GET /auth/login?return_to=` seals `{state, verifier, nonce, return_to}` into `scp_oidc` (10 minutes, `SameSite=Lax`, label
  `scp oidc v1`) and redirects to the provider with `response_type=code`, PKCE, `state`, `nonce` and `auth.oidc.scopes`.
- `GET /auth/callback`: the state must match the cookie, which is cleared on every outcome; the code is exchanged and the ID token
  verified (issuer, audience, expiry, nonce). Email, name and groups (`auth.oidc.groups_claim`, default `groups`) come from the ID
  token, the userinfo endpoint filling what it lacks. The provider's tokens are discarded; the session cookie is set and the browser
  goes to `return_to` (same-origin paths only, else `/`). A failure goes to `/signin?error=<code>`.
- The redirect URI is `server.public_url` + `/auth/callback`, so `public_url` is required with OIDC (config check).
- `POST /auth/logout` clears the session and redirects to the provider's `end_session_endpoint` (with `post_logout_redirect_uri` =
  `public_url`) when it has one, else to `/`.
- Who may sign in is the provider's business (its application assignment); SCP admits every verified identity as a viewer and grants
  more only by the lists above.

## Tokens
- `auth.tokens[]`: `{name, secret_file, groups, admin}`. The secret is at least 32 characters; requests compare SHA-256 digests in
  constant time. The viewer is `{user: "token:<name>", groups}`.
- `POST /auth/token` (form field `token`) exchanges a token for a session, for a person without a provider (krantho's token mode).
- Tokens are for the API, Markdown and badges; the page proxy also accepts them, so an agent can read the pages.

## CSRF
State-changing routes (`POST /auth/*`, the assistant) accept a request only when `Sec-Fetch-Site` is `same-origin` or absent with an
`Origin` equal to `public_url`'s origin; JSON routes also require `Content-Type: application/json`. Bearer requests carry no cookie and
are exempt.

## API and web
- `GET /api/v1/me` (contract): `{signedIn, name, via, private, admin, signIn: {oidc, token}}`; never an email of another viewer.
- `GET /api/v1/admin/status` (admins only): the operator status of the admin listener's `/api/v1/status`.
- Web: a "Sign in" link back to the page, or the viewer's name with "Admin" (administrators) and "Sign out" (not for a proxy's viewer)
  in the header (`ViewerMenu`, from `/api/v1/me`); `/signin` with the provider button and the token form, the failure explained;
  `/admin` with the status, asked for only when `/me` says admin, and the assistant's provider (assistant.md). Pages of a signed-in
  viewer are `Cache-Control: private`.
- The page proxy passes the viewer to the web UI server as `X-Scp-Viewer` (`public`, `private` or `admin`), `X-Scp-Name` and
  `X-Scp-Via`; the page CSP's `form-action` admits the issuer's origin, since sign-out's redirect to the end session is held to it.
  The provider sends the browser back to `public_url/`.

## Config
| Key | Default | |
|---|---|---|
| `auth.session_key_file` | `""` | turns sign-in on |
| `auth.session_ttl` | `12h` | |
| `auth.admin_emails`, `auth.admin_groups` | `[]` | |
| `auth.tokens[]` | `[]` | `name`, `secret_file`, `groups`, `admin` |
| `auth.oidc.issuer` | `""` | turns OIDC on; needs `client_id` and `server.public_url` |
| `auth.oidc.client_id`, `auth.oidc.client_secret_file` | `""` | no secret: a public client with PKCE |
| `auth.oidc.scopes` | `[openid, email, profile]` | `groups` added by the operator when the provider needs it |
| `auth.oidc.groups_claim` | `groups` | |

## Tests
- Sealing: round trip, tampering, a cookie of another name, expiry, key rotation.
- Middleware: the order of the four sources, a wrong bearer is 401, roles from the lists, cache headers, the CSRF rule.
- OIDC against a fake provider (`internal/auth/oidctest`: discovery, JWKS, authorize, token, userinfo, end session) on loopback:
  login, callback, a forged state, a wrong nonce, an expired ID token, groups from userinfo, logout to the end-session URL.
- End to end (`cmd/scp`): signed in through the fake provider a member of a private group sees a private repository; an admin reads
  `/api/v1/admin/status`; a token reads `/llms.txt` with its groups.
