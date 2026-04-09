-- name: DeleteChirp :exec
DELETE from chirps
WHERE id = $1;