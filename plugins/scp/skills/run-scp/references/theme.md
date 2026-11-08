# Theming a portal

A portal can wear its owner's colours, logo and name without a build: the release binary, bundle and image stay the signed ones, and
the deployment's own files say how it looks. It needs a release that has the operator guide's section 2.7 (*A theme for your
deployment*; keys `web.theme_dir` and `web.product_name`, released after v2.2.10): read that section of the running version first.
An older portal refuses the keys at `scp config check`, so upgrade it before theming.

## What to ask

Only what you cannot infer, in one go: the name the pages should use, the brand colour (a hex value or the brand guide), a logo as
SVG (and a favicon, the same file will do), and whether the dark theme matters (it does: people choose it). Colours of the portal's
signals (activity, health, AI, danger) keep their meaning: leave them alone unless the owner insists.

Never put a customer's logo, colours or name in the SCP repository or in an issue there; they belong in the deployment's own
repository, which is the owner's.

## The deployment repository

```
acme-portal/
├── scp.yaml            web.theme_dir and web.product_name beside the sources and groups
├── theme/
│   ├── theme.css       the colour tokens
│   ├── logo.svg        replaces the mark in the header (needs xmlns="http://www.w3.org/2000/svg")
│   └── favicon.svg
├── secrets/            never committed
└── compose.yaml        the release image with theme/ mounted read-only
```

The portal serves `theme/` under `/theme/` (stylesheets and pictures only), also before sign-in. No fonts, no `@import`, no address
on another host: the content security policy refuses them, so a theme uses the system fonts.

## theme.css

Set each token in `:root:not(.dark)` (light) and in `.dark` (dark). The theme loads after the portal's stylesheet, so a token set in plain
`:root` would win in dark mode too. Derive the brand colour in OKLCH (`oklch(L C H)`):

- light `--primary`: lightness 0.45 to 0.55 with `--primary-foreground` near white (`oklch(0.99 0 0)`), at least 4.5:1 between them;
- dark `--primary`: lightness 0.78 to 0.85, the same hue and chroma, with `--primary-foreground` near black;
- `--ring` the same colour as `--primary` (a little lighter in light mode);
- optionally `--accent` (the active chip's tint, light in light mode, dark in dark mode), `--background`, `--glow` and `--radius`.

```css
:root:not(.dark) { --primary: oklch(0.5 0.17 150); --primary-foreground: oklch(0.99 0 0); --ring: oklch(0.55 0.17 150); }
.dark            { --primary: oklch(0.8 0.16 150); --primary-foreground: oklch(0.15 0.03 150); --ring: oklch(0.8 0.16 150); }
:root            { --radius: 0.25rem; }
```

The operator guide's table lists every token and what it paints.

## Mount it

- **Compose**: add `- ./theme:/etc/scp/theme:ro` to the portal's `volumes`, and in `scp.yaml` `web: {theme_dir: /etc/scp/theme, product_name: Acme Code}`.
- **Kubernetes**: a ConfigMap of the three files mounted at `/etc/scp/theme` (they are text; a ConfigMap holds 1 MiB), or a volume.
- **A binary under systemd**: a directory the service reads, such as `/etc/scp/theme`.
- To ship one artifact instead: `FROM ghcr.io/cantara/source-code-portal:<version>`, `COPY theme /etc/scp/theme`.

The files are read when the portal starts: restart after a change.

## Prove it

1. `scp config check -c scp.yaml` exits 0 (a missing directory, a `theme.css` over 128 KiB or not UTF-8 and a name over 60 characters are
   listed with their key).
2. `curl -sI https://portal.example.org/theme/theme.css` is 200 with `text/css`; the dashboard's HTML has a `<link rel="stylesheet" href="/theme/theme.css?v=...">`
   after the portal's own and the title carries the name.
3. Look at the dashboard, a repository page, `/present` and `/signin` in both themes (the theme menu in the header). Show the owner a
   screenshot of each theme. Check the contrast of what you changed.

| Symptom | Cause |
|---|---|
| `/theme/theme.css` is 404 | `web.theme_dir` not set, the directory not mounted where the key says, or the file not named exactly `theme.css` |
| Colours are right in light and wrong in dark | a token set in plain `:root`: use `:root:not(.dark)` and `.dark` |
| The logo does not show | the file is not called `logo.svg`, is over 1 MiB, or the SVG has no `xmlns` |
| A change does not show | restart the portal (the files are read at the start) |
| The name is unchanged in the guide or the assistant | by design: they describe the product, not the deployment |
