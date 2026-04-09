-- name: UpgradeToChirpyRed :exec
UPDATE users SET is_chirpy_red = TRUE, updated_at = NOW()
WHERE ID = $1;