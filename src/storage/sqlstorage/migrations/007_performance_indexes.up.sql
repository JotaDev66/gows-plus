-- Partial index on (jid, timestamp) for is_real=true rows only.
-- Nearly all message queries filter is_real = true, so this index is smaller
-- and more selective than the full (jid, timestamp) index.
CREATE INDEX gows_messages_jid_timestamp_real_idx
ON gows_messages (jid, timestamp DESC)
WHERE is_real = true;

-- Reverse lookup index: pn → lid.
-- Allows efficient resolution of all LID variants for a given phone number,
-- replacing per-row correlated subqueries with a single pre-lookup.
CREATE INDEX IF NOT EXISTS whatsmeow_lid_map_pn_idx ON whatsmeow_lid_map (pn);
