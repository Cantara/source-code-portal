# A theme for a deployment (TH1)

Status: built (plan.md section 4, P-108). Binding order: ../plan.md > this note.

## Goal

A deployment looks like its owner's without rebuilding the portal: its colours, its mark and its name come from files and two keys of
the deployment's own configuration, and the release binary, the web bundle and the image stay the signed ones. The deployment's
repository holds `scp.yaml`, `theme/theme.css`, a logo and a favicon, and starts `FROM` the release image; SCP names no customer.

## Acceptance (observable)

1. `web.theme_dir` names a directory; at the start the portal reads `theme.css`, `logo.svg` and `favicon.svg` from it (each optional)
   and serves the directory's files under `/theme/` (stylesheets and pictures only), with a content hash in each URL the pages use.
2. The pages link `theme.css` after the portal's own stylesheet, so a `:root` or `.dark` block that sets the portal's colour tokens
   wins in light and dark; the header shows `logo.svg` in place of the built-in mark; the tab shows `favicon.svg`.
3. `web.product_name` replaces "Source Code Portal" in the pages' titles, the header, the search description, the social and
   structured data and the presentation page's web app manifest. The guide, the assistant and the agents' texts keep the product's name.
4. The content security policy is unchanged: the stylesheet and pictures are the portal's own (`style-src 'self'`, `img-src 'self'`);
   no font, no remote URL.
5. `/theme/` is open when sign-in is required (the sign-in page needs its theme); it serves only regular files of the directory with
   an allowed extension (`.css`, `.svg`, `.png`, `.jpg`, `.webp`, `.ico`), never a listing, a dotfile, a parent or a symlink out.
6. A bad key fails `scp config check`: a missing directory, a `theme.css` over 128 KiB or not UTF-8, a name over 60 characters or
   with a control character. Without the keys nothing changes: no route, no link, the built-in look.
7. The theme is read at the start: a change to the files needs a restart (the hash in the URLs says what the pages use).

## Design

- `internal/theme` (core): `Load(dir, name)` validates and hashes the three known files and returns a `Theme`; `Theme.Env()` is the JSON
  the web UI server receives as `SCP_THEME` (`{"name","css","logo","favicon"}`, the URLs `/theme/<file>?v=<hash>`); `Theme.Handler()` is
  `GET /theme/{file...}`: `fs.ValidPath` and `os.OpenRoot` (a path or a symbolic link cannot leave the directory), regular files only, the extension allowlist, `Cache-Control: no-cache`
  with `Last-Modified` and conditional requests (`http.ServeContent`), `X-Content-Type-Options: nosniff` (the middleware's), and the
  default API content security policy (`default-src 'none'; sandbox`) so a script in an SVG never runs.
- `cmd/scp` wires it: the route when a directory is set, the environment variable for the web UI server (`webui.New` takes it).
- `web/src/lib/look.ts` (`look`, named so beside `lib/theme.ts`, the light and dark choice) reads `SCP_THEME` defensively (a URL must start
  with `/theme/`, a name is trimmed and bounded); the root layout puts the stylesheet link (`precedence="theme"`, after the portal's own
  styles), the logo and the favicon in; the client components that print the name read it from `LookProvider`. The app manifest is
  dynamic, so it sees the running server's environment.
- A token a theme sets in plain `:root` also wins in dark mode (the theme comes later): the guide tells authors to use
  `:root:not(.dark)` and `.dark` (seen in the real-browser check).
- The portal's colour tokens (`--background`, `--primary`, the activity, announcement and diff tokens, `--radius`) are the stable
  surface of a theme: the operator guide lists them. A theme sets them in `:root` and `.dark`; it does not restyle components.

## Not in this step (and why)

- Fonts: the policy has no `font-src` and the bundle's budget has none; a theme uses the system stack. Adding `font-src 'self'` is a
  step of its own when someone needs it.
- A theme per viewer, a theme editor in the Admin page, the name in the Markdown and llms outputs: nothing needs them yet.

## Definition of done

Code and tests (the route, the validation, the web UI's reading, the layout), a real-browser look with a sample theme in light and
dark, the full gate green, the operator guide (section 2.7 and the keys), the example configuration, the plugin's references, the
CHANGELOG, `CLAUDE.md`'s code map, PR open and CI green.
