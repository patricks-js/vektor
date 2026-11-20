CREATE TABLE rollup_dirty (
  bucket_start_ms INTEGER PRIMARY KEY
) STRICT;

-- Older databases could prune part of the source for a closed 5m or 1h window.
WITH tiers(source_s, target_s, window_ms) AS (
  VALUES (30, 300, 300000), (300, 3600, 3600000)
), prefixes AS (
  SELECT w.closed_through_ms AS closed_through, t.window_ms,
    (SELECT MIN(bucket_start_ms) FROM metric_buckets WHERE resolution_s = t.source_s) AS oldest
  FROM tiers t JOIN rollup_state w ON w.resolution_s = t.target_s
)
INSERT INTO system_meta(key, value)
SELECT 'raw_pruned_before_ms', CAST(MAX(MIN(closed_through, COALESCE(
  oldest + (window_ms - ((oldest % window_ms + window_ms) % window_ms)) % window_ms,
  closed_through
))) AS TEXT)
FROM prefixes
HAVING COUNT(*) > 0;
