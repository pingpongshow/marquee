-- +goose Up
-- When a scan last found a media file gone (its library folder reachable, the file not in
-- it); NULL while the file is there. Scans remove files missing for longer than the
-- "Remove missing titles after" setting. Files already unavailable count from now.
ALTER TABLE media_files ADD COLUMN missing_since TEXT;
UPDATE media_files SET missing_since = strftime('%Y-%m-%dT%H:%M:%fZ', 'now') WHERE available = 0;

-- +goose Down
ALTER TABLE media_files DROP COLUMN missing_since;
