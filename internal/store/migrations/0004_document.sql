-- Documents (design/documents.md): what administrators edit in the pages (the layouts, the announcements), as JSON text under a name.
-- The revision is the content digest; a save names the revision it read, so a stale save is refused. Additive.
CREATE TABLE document (
  name       TEXT PRIMARY KEY,
  body       BLOB NOT NULL,
  revision   TEXT NOT NULL,
  updated_at INTEGER NOT NULL
) STRICT;
