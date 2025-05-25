-- Drop triggers
DROP TRIGGER IF EXISTS update_chapters_updated_at ON chapters;
DROP TRIGGER IF EXISTS update_research_projects_updated_at ON research_projects;
DROP TRIGGER IF EXISTS update_users_updated_at ON users;

-- Drop trigger function
DROP FUNCTION IF EXISTS update_updated_at_column;

-- Drop indexes
DROP INDEX IF EXISTS idx_generated_documents_project_id;
DROP INDEX IF EXISTS idx_sessions_refresh_token;
DROP INDEX IF EXISTS idx_sessions_user_id;
DROP INDEX IF EXISTS idx_references_project_id;
DROP INDEX IF EXISTS idx_chapters_type;
DROP INDEX IF EXISTS idx_chapters_project_id;
DROP INDEX IF EXISTS idx_research_projects_status;
DROP INDEX IF EXISTS idx_research_projects_user_id;
DROP INDEX IF EXISTS idx_users_email;

-- Drop tables
DROP TABLE IF EXISTS generated_documents;
DROP TABLE IF EXISTS sessions;
DROP TABLE IF EXISTS "references";
DROP TABLE IF EXISTS chapters;
DROP TABLE IF EXISTS research_projects;
DROP TABLE IF EXISTS users;

-- Drop extension
DROP EXTENSION IF EXISTS "uuid-ossp";
