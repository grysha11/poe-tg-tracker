-- name: RecordFetch :exec
INSERT INTO fetch_log (hour_utc, payload_sha256, fetched_at)
VALUES (?, ?, ?)
ON DUPLICATE KEY UPDATE
    payload_sha256 = VALUES(payload_sha256),
    fetched_at     = VALUES(fetched_at);

-- name: LatestFetchBefore :one
SELECT hour_utc, payload_sha256, fetched_at FROM fetch_log
WHERE hour_utc < ?
ORDER BY hour_utc DESC
LIMIT 1;
