-- Add new columns
ALTER TABLE "references"
ADD COLUMN doi VARCHAR(255),
ADD COLUMN semantic_scholar_id VARCHAR(100),
ADD COLUMN abstract TEXT,
ADD COLUMN source_api VARCHAR(50);

-- Add unique index for (project_id, doi) where doi is not null
CREATE UNIQUE INDEX unique_project_doi ON "references" (project_id, doi) WHERE doi IS NOT NULL;

-- Add unique index for (project_id, semantic_scholar_id) where semantic_scholar_id is not null
CREATE UNIQUE INDEX unique_project_semantic_id ON "references" (project_id, semantic_scholar_id) WHERE semantic_scholar_id IS NOT NULL;
