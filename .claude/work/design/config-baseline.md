# scp.yaml as the base of what admins change (CF1)

Status: built (plan.md section 4, P-124). Binding order: ../plan.md > this note > `visibility.md`, `announcements.md`, `layout.md`,
`documents.md`.

## Goal

The configuration file holds every setting an admin can change in the pages, and the precedence is deterministic: the file is the base,
the store keeps the admins' changes, and the admin can download the file with those changes written into it, to commit and deploy, so
a store reset or lost loses nothing.

## What it is

- **The sections.** `visibility:` (the shape of the document `visibility`: `groups`, `repos`, `rules`; `search` is on unless it says
  `false`), `layouts:` (a page's layout by page, as the design mode saves it) and `announcements:` (as before). `config.Baseline`
  holds the first two as JSON, read from the file with go.yaml.in/yaml/v4 (viper lowercases map keys, which would change addresses and
  the layouts' field names), and the file itself. `cmd/scp` `baseline` parses them with the features' own parsers at `config check`
  and at the start.
- **Precedence, per item.** A repository, a product, the rules list, an announcement by id, a page's layout: the store's change, else
  the file's, else the built-in default.
- **One mechanism.** `docstore.Overlay[T, D]` (`overlay.go`): the base, the stored changes D, and the feature's `Ops` (`Merge`,
  `Diff`, `Count`, `Parse`). `Save` stores `Diff(base, value)`, so a value equal to the file's stores no change; `Value` is the value in
  force; `Changes` counts what is kept only in the store. Visibility, the announcements and each page's layout are overlays.
- **What the store keeps.** `visibility.Overrides` (a setting, or `null` that drops the file's choice so the item inherits; `rules`
  present replaces the file's list; `version: 2`), `announce.List` (the announcements changed or added here, `removed` the file's ids
  taken away), a page's layout (the empty layout is no change; a reset saves the file's).
- **At the start** a stored change equal to the file is dropped (`OpenOverlay`), so after the downloaded file is deployed the store
  holds no changes and the next change made in git takes effect. An overlay without changes keeps an empty document, never none: what
  is imported only into a store that lacks the document (`visibility.Import`, `layout.dir`) is never imported again.
- **Older stores.** A store of 2.2.20 or earlier holds whole documents: read as changes to an empty file. A whole `visibility` document
  (no `version`) wrote `"rules": []` for "no rules", which is no change to the file's rules; the start writes it again as overrides.
- **The status.** `/api/v1/status` `config`: `file` (the portal started from a file) and `changes` per document; one warning, worded in
  `cmd/scp` `configWarning`. Admin's overview *Configuration* card reads them and offers the download.
- **The download.** `GET /api/v1/admin/config.yaml` (admin only, `cmd/scp/configfile.go`): the file as read at the start, its
  `visibility`, `layouts` and `announcements` keys replaced by the values in force (added at the end when missing, removed when empty),
  every other line of the file kept byte for byte: the sections' lines (from the key to the blank lines and column-one comments above
  the next key) are spliced with their new text, encoded with no line width (owner, 2.2.22: re-encoding the whole file folded long
  values and dropped blank lines, noise in a diff); the values written from JSON in their fields' order (`nodeOf`). A portal started
  without a file (`SCP_*` variables alone) answers 409 `no-file`: the three sections alone are not a configuration that starts.
- **The marks.** Who sees what says per row *set in scp.yaml*, *set here* or *changed here; scp.yaml: …* with *reset to scp.yaml*, and
  for the rules; the announcements are *from scp.yaml*, *changed here* (*Reset to scp.yaml*), *added here*, or *in scp.yaml, removed
  here* (*Restore from scp.yaml*); the design mode says whether the page shows scp.yaml's layout and resets to it.

## What it does not do

- The other keys (sources, sign-in, access, the rest) are only in the file; nothing writes the file on the server.
- `layout.dir` stays (a key removed would refuse a running deployment's file); it imports only into an empty store.

## Acceptance

1. The file's `visibility:` decides with nothing stored; a stored change wins; `null` returns an item to inheriting; a change equal to
   the file is dropped at the start (`internal/feature/visibility` `TestConfigurationIsTheBaseline`).
2. The file's announcements can be changed and removed, kept as changes; a removal the file no longer needs is dropped
   (`internal/feature/announce` `TestAdminRoutes`).
3. The file's layout renders a page; a change wins and is warned about; a reset returns to the file's (`internal/feature/layout`
   `TestConfigurationIsTheBaseline`).
4. The download keeps the operator's comments and keys and holds what is in force; loaded as the configuration, its base is that
   (`cmd/scp` `TestConfigurationIsTheBaseline`, `TestConfigYAML`); the sections are read as written (`internal/platform/config`
   `TestBaselineIsReadAsWritten`).
5. Admin marks and resets changes against the file (`visibility.test.tsx`, `announcements.test.ts`, `design.test.tsx`); the overview
   offers the download, which only an admin gets (`signin.test.tsx`, `web/e2e/visibility.spec.ts`).
