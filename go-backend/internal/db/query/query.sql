-- name: CreateUser :one
INSERT INTO users (
    email, password_hash, first_name, last_name
) VALUES (
    $1, $2, $3, $4
) RETURNING *;

-- name: GetUserByEmail :one
SELECT * FROM users
WHERE email = $1 LIMIT 1;

-- name: GetUserByID :one
SELECT * FROM users
WHERE id = $1 LIMIT 1;

-- name: UpdateUserVerificationStatus :one
UPDATE users
SET is_verified = $2, updated_at = NOW()
WHERE id = $1
RETURNING *;

-- name: CreateResearchProject :one
INSERT INTO research_projects (
    user_id, title, specialization, university, description
) VALUES (
    $1, $2, $3, $4, $5
) RETURNING *;

-- name: GetUserResearchProjects :many
SELECT * FROM research_projects
WHERE user_id = $1
ORDER BY created_at DESC;

-- name: GetResearchProjectByID :one
SELECT * FROM research_projects
WHERE id = $1 AND user_id = $2 LIMIT 1;

-- name: UpdateResearchProject :one
UPDATE research_projects
SET title = $2, specialization = $3, university = $4, description = $5, status = $6, updated_at = NOW()
WHERE id = $1 AND user_id = $7
RETURNING *;

-- name: UpdateResearchProjectStatus :one
UPDATE research_projects
SET status = $2, updated_at = NOW()
WHERE id = $1 AND user_id = $3
RETURNING *;

-- name: DeleteResearchProject :exec
DELETE FROM research_projects
WHERE id = $1 AND user_id = $2;

-- name: CreateChapter :one
INSERT INTO chapters (
    project_id, type, title, content, word_count
) VALUES (
    $1, $2, $3, $4, $5
) RETURNING *;

-- name: GetChapterByID :one
SELECT * FROM chapters
WHERE id = $1 LIMIT 1;

-- name: GetChaptersByProjectID :many
SELECT * FROM chapters
WHERE project_id = $1
ORDER BY
    CASE type
        WHEN 'introduction' THEN 1
        WHEN 'literature_review' THEN 2
        WHEN 'methodology' THEN 3
        WHEN 'results' THEN 4
        WHEN 'conclusion' THEN 5
        ELSE 6
    END;

-- name: GetChapterByProjectIDAndType :one
SELECT * FROM chapters
WHERE project_id = $1 AND type = $2 LIMIT 1;

-- name: UpdateChapter :one
UPDATE chapters
SET title = $2, content = $3, word_count = $4, status = $5, updated_at = NOW()
WHERE chapters.id = $1 AND project_id = (SELECT project_id FROM research_projects WHERE research_projects.id = $6 AND user_id = $7) -- ensure user owns project
RETURNING *;

-- name: DeleteChapter :exec
DELETE FROM chapters
WHERE chapters.id = $1 AND project_id = (SELECT project_id FROM research_projects WHERE research_projects.id = $2 AND user_id = $3);


-- name: CreateReference :one
INSERT INTO "references" ( -- Quoted
    project_id, title, authors, journal, publication_year, doi, url, citation_apa, citation_mla
) VALUES (
    $1, $2, $3, $4, $5, $6, $7, $8, $9
) RETURNING *;

-- name: GetReferencesByProjectID :many
SELECT * FROM "references" -- Quoted
WHERE project_id = $1
ORDER BY created_at DESC;

-- name: DeleteReference :exec
DELETE FROM "references" -- Quoted
WHERE id = $1 AND project_id = $2;
-- Ensure user owns project for delete if needed, or handled at service layer

-- name: CreateSession :one
INSERT INTO sessions (
    id, user_id, refresh_token, user_agent, client_ip, is_blocked, expires_at
) VALUES (
    $1, $2, $3, $4, $5, $6, $7
) RETURNING *;

-- name: GetSessionByRefreshToken :one
SELECT * FROM sessions
WHERE refresh_token = $1 LIMIT 1;

-- name: DeleteSessionByRefreshToken :exec
DELETE FROM sessions
WHERE refresh_token = $1;

-- name: BlockSession :one
UPDATE sessions
SET is_blocked = TRUE
WHERE id = $1
RETURNING *;

-- name: CreateGeneratedDocument :one
INSERT INTO generated_documents (
    project_id, file_name, file_path, file_size, mime_type
) VALUES (
    $1, $2, $3, $4, $5
) RETURNING *;

-- name: GetGeneratedDocumentsByProjectID :many
SELECT * FROM generated_documents
WHERE project_id = $1
ORDER BY created_at DESC;

-- name: GetGeneratedDocumentByID :one
SELECT * FROM generated_documents
WHERE id = $1 LIMIT 1;

-- name: UpdateGeneratedDocumentStatus :one
UPDATE generated_documents
SET status = $2
WHERE id = $1
RETURNING *;

-- name: UpdateGeneratedDocument :one
UPDATE generated_documents
SET file_name = $2, file_path = $3, file_size = $4, mime_type = $5, status = $6
WHERE id = $1
RETURNING *;

-- name: DeleteGeneratedDocument :exec
DELETE FROM generated_documents
WHERE id = $1;