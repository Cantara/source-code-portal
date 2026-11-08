# Everything persisted in SQLite, memory for speed; an instance that dies survives (SQ1)

Status: built (plan.md section 4, P-97). Binding order: ../plan.md > this note > `layout.md`, `announcements.md`, `sync-snapshot-store.md`.

Owner, 2026-11-08: all data is persisted in SQLite and memory loads it for fast access, kept simple and clear; if an instance of SCP
dies it must survive: a hard requirement.

## The rule

- **One place for data: the store (SQLite).** Everything the portal holds on to is a row in it: the catalogue and what the jobs read
  (`repo`, `item`), parked webhook events and the sync timetable (`meta`), sealed credentials (`credential`), and what administrators
  edit in the pages (`document`: the layouts and the announcements). Memory is only ever a copy loaded from it (the snapshot, the
  search index, a document's value) and is rebuilt at the start.
- **Not data, by design**: sessions are sealed cookies (nothing to lose, the key is `auth.session_key_file`), the search index and the
  snapshot are derived, and the configuration is the operator's file.
- **Survives a death.** An acknowledged write is synced to disk before it returns (`DB.durably`: `PRAGMA synchronous = FULL` on that
  connection): parked webhook events, sealed credentials, saved documents. The jobs' own writes are WAL with `synchronous = NORMAL`:
  a process that is killed loses none of them (the OS has them), and a power cut can lose the last seconds of a cache that the next
  read of GitHub refills. The start is always a warm start from the store.

## Documents

`document (name, body, revision, updated_at)`, in the baseline `0001` (SC1). `internal/docstore`:

- `Open` loads a document into memory; `Get` never touches the store; `Save` and `Remove` write through, then update memory. The
  revision is the content digest; a save names the revision it read, and one based on an older read is refused (412), as before.
- The write and the revision check are one step under the store's lock, so two admins never overwrite each other.
- A document that does not parse (a later version tightened what it accepts) is served as the built-in value with the problem in the
  status; an admin can save over it.
- Layouts are saved always: there is no read-only mode except tests with no store.

### Upgrading an instance that kept files

`layout.dir` used to be the directory of `dashboard.json`, `present.json` and `announcements.json`. It stays as a key (so no
configuration breaks) with one job: at the start each name the store does not hold yet is imported from `<dir>/<name>.json`, once; the
log names what was taken. After that the store is the truth and the files are not read. A directory that is gone is fine (nothing to
import). Hand-editing files and a volume shared between instances stop working: edit in the design mode or the Admin page.

## Acceptance

- Unit: documents are revisioned, kept across a reopening without a close, refused on a stale revision; a broken document falls back
  with its problem and can be overwritten; no store is read-only; the import takes files once and never overwrites a stored document.
- `scripts/crash-rehearsal.sh` (in `make rehearse`, part of `make check`): a real instance is killed with SIGKILL the moment an
  administrator's layout and announcement saves are acknowledged, GitHub is gone and the imported file removed, and the restarted
  process serves the catalogue and both saves from the store alone.
- `scripts/upgrade-rehearsal.sh` (same target): the newest release's store migrates, the copy is taken, the older build quarantines
  the migrated store, the copy brings the data back.

## Not covered

Several instances sharing admin edits (the directory they shared is gone; each has its own store). A power cut loses at most the
jobs' last seconds, never an acknowledged save.
