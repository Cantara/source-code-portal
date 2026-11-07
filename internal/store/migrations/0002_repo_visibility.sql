-- A repository's visibility, kept beside its identity so the store describes itself and a later rule (a private repository nobody can
-- read leaves with its items) needs no payload parsing. Additive: nothing existing is rewritten (design/store-model.md).
ALTER TABLE repo ADD COLUMN visibility TEXT NOT NULL DEFAULT 'unknown' CHECK (visibility IN ('unknown', 'public', 'private', 'internal'));

-- Backfill from each source's last catalogue listing.
UPDATE repo SET visibility = coalesce((
  SELECT json_extract(j.value, '$.repo.visibility')
  FROM item i, json_each(i.payload, '$.repos') j
  WHERE i.key = 'catalog.reconcile/' || repo.source AND json_extract(j.value, '$.repo.id.GitHubID') = repo.github_id), 'unknown')
WHERE EXISTS (SELECT 1 FROM item WHERE key = 'catalog.reconcile/' || repo.source);
