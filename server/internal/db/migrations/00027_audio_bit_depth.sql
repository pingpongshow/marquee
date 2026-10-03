-- +goose Up
-- Bit depth of lossless audio (MUSIC-23), from the probe output already stored with each
-- file; new files get it from the scanner.
UPDATE streams SET bit_depth = (
    SELECT CAST(json_extract(j.value, '$.bits_per_raw_sample') AS INTEGER)
    FROM media_files f, json_each(f.probe_json, '$.streams') j
    WHERE f.id = streams.file_id AND json_extract(j.value, '$.index') = streams.stream_index
        AND CAST(json_extract(j.value, '$.bits_per_raw_sample') AS INTEGER) > 0)
WHERE kind = 'audio' AND bit_depth IS NULL AND stream_index IS NOT NULL
    AND codec IN ('flac', 'alac', 'ape', 'wavpack', 'tta');
UPDATE streams SET bit_depth = CASE
        WHEN codec LIKE 'pcm_%8%' THEN 8 WHEN codec LIKE 'pcm_%16%' THEN 16 WHEN codec LIKE 'pcm_%24%' THEN 24
        WHEN codec LIKE 'pcm_%32%' THEN 32 WHEN codec LIKE 'pcm_%64%' THEN 64 END
WHERE kind = 'audio' AND bit_depth IS NULL AND codec LIKE 'pcm_%';

-- +goose Down
UPDATE streams SET bit_depth = NULL WHERE kind = 'audio';
