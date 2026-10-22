-- Lossy by necessity: one row per (item, Source) is kept, so per-platform
-- evidence and streaks collapse to a single arbitrary platform. Re-applying the
-- up migration afterwards leaves game evidence on one platform (or none, for an
-- unmapped one) until the next sync of each Source rebuilds it.
DELETE FROM source_absence_state a USING source_absence_state b
WHERE a.source_id = b.source_id AND a.catalog_item_id = b.catalog_item_id AND a.ctid > b.ctid;
ALTER TABLE source_absence_state DROP CONSTRAINT source_absence_state_pkey;
ALTER TABLE source_absence_state DROP COLUMN platform_key;
ALTER TABLE source_absence_state ADD PRIMARY KEY (source_id, catalog_item_id);

DELETE FROM availability_evidence a USING availability_evidence b
WHERE a.catalog_item_id = b.catalog_item_id AND a.source_id = b.source_id AND a.ctid > b.ctid;
ALTER TABLE availability_evidence DROP CONSTRAINT availability_evidence_pkey;
ALTER TABLE availability_evidence DROP COLUMN platform_key;
ALTER TABLE availability_evidence ADD PRIMARY KEY (catalog_item_id, source_id);
