-- Drop unique indexes first
DROP INDEX IF EXISTS unique_project_doi;
DROP INDEX IF EXISTS unique_project_semantic_id;

-- Remove the added columns
ALTER TABLE "references"
DROP COLUMN IF EXISTS doi,
DROP COLUMN IF EXISTS semantic_scholar_id,
DROP COLUMN IF EXISTS abstract,
DROP COLUMN IF EXISTS source_api;
