-- A game can be present on several platforms of one Source (RomM keeps one ROM
-- per platform), and a Request names the platforms it accepts, so evidence and
-- absence streaks are tracked per platform. platform_key is the IGDB platform
-- key ("igdb:130"), or '' for non-game media and for platforms a Source cannot
-- map to IGDB.
ALTER TABLE availability_evidence ADD COLUMN platform_key TEXT NOT NULL DEFAULT '';
ALTER TABLE availability_evidence DROP CONSTRAINT availability_evidence_pkey;
ALTER TABLE availability_evidence ADD PRIMARY KEY (catalog_item_id, source_id, platform_key);

ALTER TABLE source_absence_state ADD COLUMN platform_key TEXT NOT NULL DEFAULT '';
ALTER TABLE source_absence_state DROP CONSTRAINT source_absence_state_pkey;
ALTER TABLE source_absence_state ADD PRIMARY KEY (source_id, catalog_item_id, platform_key);
