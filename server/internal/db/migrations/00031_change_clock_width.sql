-- +goose Up
-- change_clock times become fixed width (nanoseconds always written) so text comparison
-- orders them; earlier rows dropped trailing zeros.
UPDATE change_clock SET at = strftime('%Y-%m-%dT%H:%M:%f', at) || '000000Z' WHERE at NOT LIKE '%.%________Z' OR length(at) != 30;

-- +goose Down
SELECT 1;
