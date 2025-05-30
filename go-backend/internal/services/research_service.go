package services

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/shawgichan/research-service/go-backend/internal/db"
	"github.com/shawgichan/research-service/go-backend/internal/db/sqlc"
	"github.com/shawgichan/research-service/go-backend/internal/models"

	applogger "github.com/shawgichan/research-service/go-backend/internal/logger"
	apimodels "github.com/shawgichan/research-service/go-backend/internal/models" // API models

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
)

var (
	ErrProjectNotFound      = errors.New("project not found or access denied")
	ErrChapterNotFound      = errors.New("chapter not found or access denied")
	ErrChapterAlreadyExists = errors.New("chapter of this type already exists for the project")
	ErrReferenceNotFound    = errors.New("reference not found or access denied")
	ErrDocumentNotFound     = errors.New("document not found or access denied")
)

type ResearchService struct {
	store     db.Store
	aiService *AIService
	logger    *applogger.AppLogger
}

// Add Python service URL to config or as a constant
const pythonDocGenServiceURL = "http://localhost:8001" // Change if using Docker Compose service name
const OUTPUT_DIR_FOR_GO = ""

type PythonDocGenRequest struct {
	ProjectID         uuid.UUID              `json:"project_id"`
	ResearchTitle     string                 `json:"research_title"`
	StudentName       string                 `json:"student_name,omitempty"`
	UniversityName    string                 `json:"university_name,omitempty"`
	Specialization    string                 `json:"specialization,omitempty"`
	Chapters          []PythonChapterData    `json:"chapters"`
	References        []PythonReferenceData  `json:"references,omitempty"`
	FormattingOptions map[string]interface{} `json:"formatting_options,omitempty"`
}
type PythonChapterData struct {
	Type    string `json:"type"`
	Title   string `json:"title"`
	Content string `json:"content"`
}
type PythonReferenceData struct {
	CitationAPA string `json:"citation_apa,omitempty"`
}
type PythonDocGenResponse struct { // Matches Python service response
	ProjectID uuid.UUID `json:"project_id"`
	FileName  string    `json:"file_name"`
	Message   string    `json:"message"`
}

func NewResearchService(store db.Store, aiService *AIService, logger *applogger.AppLogger) *ResearchService {
	return &ResearchService{
		store:     store,
		aiService: aiService,
		logger:    logger,
	}
}

func (s *ResearchService) CreateProject(ctx context.Context, userID uuid.UUID, req apimodels.CreateProjectRequest) (sqlc.ResearchProject, error) {
	s.logger.Info("Creating project", "userID", userID, "title", req.Title)
	params := sqlc.CreateResearchProjectParams{
		UserID:         pgtype.UUID{Bytes: userID, Valid: true},
		Title:          req.Title,
		Specialization: req.Specialization,
		University:     pgtype.Text{String: req.University, Valid: req.University != ""},
		Description:    pgtype.Text{String: req.Description, Valid: req.Description != ""},
		// Status defaults to 'draft' in DB
	}
	project, err := s.store.CreateResearchProject(ctx, params)
	if err != nil {
		s.logger.Error("Failed to create project in DB", "userID", userID, "title", req.Title, "error", err)
		return sqlc.ResearchProject{}, fmt.Errorf("could not create project: %w", err)
	}
	s.logger.Info("Project created successfully", "projectID", project.ID, "userID", userID)
	return project, nil
}

func (s *ResearchService) GetUserProjectByID(ctx context.Context, projectID, userID uuid.UUID) (sqlc.ResearchProject, error) {
	s.logger.Info("Fetching project by ID", "projectID", projectID, "userID", userID)
	project, err := s.store.GetResearchProjectByID(ctx, sqlc.GetResearchProjectByIDParams{ID: pgtype.UUID{Bytes: projectID, Valid: true}, UserID: pgtype.UUID{Bytes: userID, Valid: true}})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) || errors.Is(err, sql.ErrNoRows) {
			s.logger.Warn("Project not found or access denied", "projectID", projectID, "userID", userID)
			return sqlc.ResearchProject{}, ErrProjectNotFound
		}
		s.logger.Error("Failed to get project by ID from DB", "projectID", projectID, "userID", userID, "error", err)
		return sqlc.ResearchProject{}, fmt.Errorf("database error fetching project: %w", err)
	}
	return project, nil
}

func (s *ResearchService) GetUserProjects(ctx context.Context, userID uuid.UUID) ([]sqlc.ResearchProject, error) {
	s.logger.Info("Fetching all projects for user", "userID", userID)
	projects, err := s.store.GetUserResearchProjects(ctx, pgtype.UUID{Bytes: userID, Valid: true})
	if err != nil {
		s.logger.Error("Failed to get user projects from DB", "userID", userID, "error", err)
		return nil, fmt.Errorf("database error fetching projects: %w", err)
	}
	if projects == nil { // sqlc might return nil slice if no rows
		return []sqlc.ResearchProject{}, nil
	}
	return projects, nil
}

func (s *ResearchService) UpdateProject(ctx context.Context, projectID, userID uuid.UUID, req apimodels.UpdateProjectRequest) (sqlc.ResearchProject, error) {
	s.logger.Info("Updating project", "projectID", projectID, "userID", userID)
	// First, get the existing project to ensure it belongs to the user and to get current values
	existingProject, err := s.GetUserProjectByID(ctx, projectID, userID)
	if err != nil {
		return sqlc.ResearchProject{}, err // ErrProjectNotFound will be returned from GetUserProjectByID
	}

	params := sqlc.UpdateResearchProjectParams{
		ID:             pgtype.UUID{Bytes: projectID, Valid: true},
		UserID:         pgtype.UUID{Bytes: userID, Valid: true},
		Title:          existingProject.Title,
		Specialization: existingProject.Specialization,
		University:     existingProject.University,
		Description:    existingProject.Description,
		Status:         existingProject.Status,
	}

	if req.Title != nil {
		params.Title = *req.Title
	}
	if req.Specialization != nil {
		params.Specialization = *req.Specialization
	}
	if req.University != nil {
		params.University = pgtype.Text{String: *req.University, Valid: *req.University != ""}
	}
	if req.Description != nil {
		params.Description = pgtype.Text{String: *req.Description, Valid: *req.Description != ""}
	}
	if req.Status != nil {
		params.Status = pgtype.Text{String: *req.Status, Valid: *req.Status != ""}
	}

	updatedProject, err := s.store.UpdateResearchProject(ctx, params)
	if err != nil {
		s.logger.Error("Failed to update project in DB", "projectID", projectID, "userID", userID, "error", err)
		return sqlc.ResearchProject{}, fmt.Errorf("could not update project: %w", err)
	}
	s.logger.Info("Project updated successfully", "projectID", updatedProject.ID)
	return updatedProject, nil
}

func (s *ResearchService) DeleteProject(ctx context.Context, projectID, userID uuid.UUID) error {
	s.logger.Info("Deleting project", "projectID", projectID, "userID", userID)
	// Optional: Check if project exists and belongs to user first
	// _, err := s.GetUserProjectByID(ctx, projectID, userID)
	// if err != nil {
	// 	return err
	// }
	err := s.store.DeleteResearchProject(ctx, sqlc.DeleteResearchProjectParams{ID: pgtype.UUID{Bytes: projectID, Valid: true}, UserID: pgtype.UUID{Bytes: userID, Valid: true}})
	if err != nil {
		s.logger.Error("Failed to delete project from DB", "projectID", projectID, "userID", userID, "error", err)
		return fmt.Errorf("could not delete project: %w", err)
	}
	s.logger.Info("Project deleted successfully", "projectID", projectID)
	return nil
}

// --- Chapter Methods ---

func (s *ResearchService) CreateChapter(ctx context.Context, userID uuid.UUID, req apimodels.CreateChapterRequest) (sqlc.Chapter, error) {
	s.logger.Info("Creating chapter", "projectID", req.ProjectID, "type", req.Type, "userID", userID)
	// Verify user owns the project
	_, err := s.GetUserProjectByID(ctx, req.ProjectID, userID)
	if err != nil {
		s.logger.Warn("User does not own project for chapter creation", "projectID", req.ProjectID, "userID", userID)
		return sqlc.Chapter{}, ErrProjectNotFound
	}

	// Check if chapter of this type already exists for the project
	_, err = s.store.GetChapterByProjectIDAndType(ctx, sqlc.GetChapterByProjectIDAndTypeParams{
		ProjectID: pgtype.UUID{Bytes: req.ProjectID, Valid: true},
		Type:      req.Type,
	})
	if err == nil {
		s.logger.Warn("Chapter already exists for project", "projectID", req.ProjectID, "type", req.Type)
		return sqlc.Chapter{}, ErrChapterAlreadyExists
	}
	if !errors.Is(err, pgx.ErrNoRows) && !errors.Is(err, sql.ErrNoRows) {
		s.logger.Error("DB error checking existing chapter", "projectID", req.ProjectID, "type", req.Type, "error", err)
		return sqlc.Chapter{}, fmt.Errorf("db error: %w", err)
	}

	params := sqlc.CreateChapterParams{
		ProjectID: pgtype.UUID{Bytes: req.ProjectID, Valid: true},
		Type:      req.Type,
		Title:     req.Title,
		Content:   pgtype.Text{String: req.Content, Valid: req.Content != ""},
		WordCount: pgtype.Int4{Int32: int32(utf8.RuneCountInString(req.Content)), Valid: req.Content != ""}, // Basic word count
		// Status defaults to 'draft'
	}
	chapter, err := s.store.CreateChapter(ctx, params)
	if err != nil {
		s.logger.Error("Failed to create chapter in DB", "projectID", req.ProjectID, "type", req.Type, "error", err)
		return sqlc.Chapter{}, fmt.Errorf("could not create chapter: %w", err)
	}
	s.logger.Info("Chapter created successfully", "chapterID", chapter.ID)
	return chapter, nil
}

func (s *ResearchService) GetProjectChapters(ctx context.Context, projectID, userID uuid.UUID) ([]sqlc.Chapter, error) {
	s.logger.Info("Fetching chapters for project", "projectID", projectID, "userID", userID)
	// Verify user owns the project
	_, err := s.GetUserProjectByID(ctx, projectID, userID)
	if err != nil {
		return nil, ErrProjectNotFound
	}

	chapters, err := s.store.GetChaptersByProjectID(ctx, pgtype.UUID{Bytes: projectID, Valid: true})
	if err != nil {
		s.logger.Error("Failed to get project chapters from DB", "projectID", projectID, "error", err)
		return nil, fmt.Errorf("database error fetching chapters: %w", err)
	}
	if chapters == nil {
		return []sqlc.Chapter{}, nil
	}
	return chapters, nil
}

func (s *ResearchService) GetChapterByID(ctx context.Context, chapterID, userID uuid.UUID) (sqlc.Chapter, error) {
	s.logger.Info("Fetching chapter by ID", "chapterID", chapterID, "userID", userID)
	// This requires a more complex query or multiple queries to ensure user ownership through project
	// For simplicity, we assume if a chapter is requested, its project ownership is checked elsewhere or it's fine.
	// A better query would be: SELECT c.* FROM chapters c JOIN research_projects rp ON c.project_id = rp.id WHERE c.id = $1 AND rp.user_id = $2;
	// As sqlc GetChapterByID is not defined, let's mock this for now.
	// In a real app, you'd add such a query to query.sql

	// Placeholder: In a real scenario, you'd query with user ID for security.
	// This is a simplified GetChapter. You need to ensure the user owns the project this chapter belongs to.
	// For example, get the chapter, then get its project_id, then check if user owns that project.
	// Or, add a query like `GetChapterByIDAndUserID` to `query.sql`.

	// For now, let's assume the handlers ensure this via project checks first.
	// If you need direct chapter fetch with auth, add a specific query.
	s.logger.Warn("GetChapterByID needs a secure query ensuring user ownership via project.")
	return sqlc.Chapter{}, errors.New("GetChapterByID requires a secure query; not implemented directly for now")
}

func (s *ResearchService) UpdateChapter(ctx context.Context, chapterID, projectID, userID uuid.UUID, req apimodels.UpdateChapterRequest) (sqlc.Chapter, error) {
	s.logger.Info("Updating chapter", "chapterID", chapterID, "userID", userID)
	// Verify user owns the project this chapter belongs to
	_, err := s.GetUserProjectByID(ctx, projectID, userID)
	if err != nil {
		s.logger.Warn("User does not own project for chapter update", "projectID", projectID, "userID", userID)
		return sqlc.Chapter{}, ErrProjectNotFound
	}

	// Get existing chapter to update its fields
	// A query like GetChapterByIDAndProjectID would be good here.
	// For now, we rely on the UpdateChapter sqlc query which should ideally also check project ownership.
	// The provided query `UpdateChapter` does have a subquery for user check based on projectID and userID passed as $6 and $7.

	// We need current values if not all fields are updated. sqlc's UpdateChapter updates specific fields.
	// So, we need to get the chapter first to fill in non-updated fields IF the query updates all fields.
	// The sqlc UpdateChapter query you provided updates only specific fields (title, content, word_count, status).
	// So we don't strictly need to fetch it first *if* the query is designed that way.
	// However, the sqlc query is `UPDATE chapters SET title = $2, content = $3, word_count = $4, updated_at = NOW() WHERE id = $1 RETURNING *;`
	// It needs all values. So fetch first.

	// Let's get the chapter details first to ensure we have all necessary fields for the update.
	// This is a common pattern: fetch, modify, save.
	// A better query would be `GetChapterByIDAndProjectID(ctx, chapterID, projectID)`
	// For now, let's assume this check is sufficient.
	// A truly robust way needs a specific `GetChapterByIDAndProjectID` query.

	// The current sqlc query for UpdateChapter requires values for title, content, word_count, status.
	// It would be better if the sqlc UpdateChapter query accepted nullable values for each field to update only provided ones.
	// Let's assume the current query needs all fields:

	// Get current chapter
	var currentChapter sqlc.Chapter
	// This is where a GetChapterByIDAndProjectID would be useful.
	// Let's find it in the project's chapters as a workaround for now.
	chapters, err := s.store.GetChaptersByProjectID(ctx, pgtype.UUID{Bytes: projectID, Valid: true})
	if err != nil {
		return sqlc.Chapter{}, fmt.Errorf("could not fetch chapters for update: %w", err)
	}
	found := false
	for _, ch := range chapters {
		if ch.ID.Bytes == chapterID {
			currentChapter = ch
			found = true
			break
		}
	}
	if !found {
		return sqlc.Chapter{}, ErrChapterNotFound
	}

	updateParams := sqlc.UpdateChapterParams{
		ID:        pgtype.UUID{Bytes: chapterID, Valid: true},
		Title:     currentChapter.Title,
		Content:   currentChapter.Content,
		WordCount: currentChapter.WordCount,
		Status:    currentChapter.Status,
		// These are the $6 and $7 for the subquery in UpdateChapter
		ID_2:   pgtype.UUID{Bytes: projectID, Valid: true}, // Project ID for ownership check
		UserID: pgtype.UUID{Bytes: userID, Valid: true},    // User ID for ownership check
	}

	if req.Title != nil {
		updateParams.Title = *req.Title
	}
	if req.Content != nil {
		updateParams.Content = pgtype.Text{String: *req.Content, Valid: true}
		updateParams.WordCount = pgtype.Int4{Int32: int32(utf8.RuneCountInString(*req.Content)), Valid: true}
	}
	if req.Status != nil {
		updateParams.Status = pgtype.Text{String: *req.Status, Valid: true}
	}

	updatedChapter, err := s.store.UpdateChapter(ctx, updateParams)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) || errors.Is(err, sql.ErrNoRows) { // If RETURNING * found no row (e.g. subquery failed)
			s.logger.Warn("Update chapter failed, chapter not found or ownership issue", "chapterID", chapterID, "error", err)
			return sqlc.Chapter{}, ErrChapterNotFound
		}
		s.logger.Error("Failed to update chapter in DB", "chapterID", chapterID, "error", err)
		return sqlc.Chapter{}, fmt.Errorf("could not update chapter: %w", err)
	}
	s.logger.Info("Chapter updated successfully", "chapterID", updatedChapter.ID)
	return updatedChapter, nil
}

// --- AI Content Generation for Chapters ---

func (s *ResearchService) GenerateChapterContent(ctx context.Context, projectID, chapterID, userID uuid.UUID, chapterType string) (sqlc.Chapter, error) {
	s.logger.Info("Generating content for chapter", "chapterID", chapterID, "projectID", projectID, "type", chapterType, "userID", userID)
	project, err := s.GetUserProjectByID(ctx, projectID, userID)
	if err != nil {
		return sqlc.Chapter{}, err // Project not found or access denied
	}

	// Find the chapter
	chapters, err := s.store.GetChaptersByProjectID(ctx, pgtype.UUID{Bytes: projectID, Valid: true})
	if err != nil {
		return sqlc.Chapter{}, fmt.Errorf("could not fetch chapters: %w", err)
	}
	var targetChapter sqlc.Chapter
	found := false
	for _, ch := range chapters {
		if ch.ID.Bytes == chapterID && ch.Type == chapterType {
			targetChapter = ch
			found = true
			break
		}
	}
	_ = targetChapter // for now, we're not using this
	if !found {
		s.logger.Warn("Chapter not found for content generation", "chapterID", chapterID, "projectID", projectID, "type", chapterType)
		return sqlc.Chapter{}, ErrChapterNotFound
	}

	var generatedContent string
	var generatedReferences []*apimodels.ReferenceResponse // For lit review

	switch chapterType {
	case "literature_review":
		generatedContent, generatedReferences, err = s.aiService.GenerateLiteratureReview(ctx, project.Title, project.Specialization)
		if err == nil && len(generatedReferences) > 0 {
			// Save these references to the DB
			for _, refData := range generatedReferences {
				// Check if refData fields are nil before dereferencing
				var authors, journal, doi, url, citationAPA, citationMLA pgtype.Text
				var pubYear pgtype.Int4

				if refData.Authors != "" {
					authors = pgtype.Text{String: refData.Authors, Valid: true}
				}
				if refData.Journal != "" {
					journal = pgtype.Text{String: refData.Journal, Valid: true}
				}
				if refData.DOI != "" {
					doi = pgtype.Text{String: refData.DOI, Valid: true}
				}
				if refData.URL != "" {
					url = pgtype.Text{String: refData.URL, Valid: true}
				}
				if refData.CitationAPA != "" {
					citationAPA = pgtype.Text{String: refData.CitationAPA, Valid: true}
				}
				if refData.CitationMLA != "" {
					citationMLA = pgtype.Text{String: refData.CitationMLA, Valid: true}
				}
				if refData.PublicationYear != 0 {
					pubYear = pgtype.Int4{Int32: int32(refData.PublicationYear), Valid: true}
				}

				_, refErr := s.store.CreateReference(ctx, sqlc.CreateReferenceParams{
					ProjectID:       pgtype.UUID{Bytes: projectID, Valid: true},
					Title:           refData.Title, // Assuming Title is not nil
					Authors:         authors,
					Journal:         journal,
					PublicationYear: pubYear,
					Doi:             doi,
					Url:             url,
					CitationApa:     citationAPA,
					CitationMla:     citationMLA,
				})
				if refErr != nil {
					s.logger.Error("Failed to save generated reference", "projectID", projectID, "error", refErr)
					// Continue, but log the error
				}
			}
		}
	case "introduction":
		// For introduction, we might need summary of lit review.
		// Fetch lit review chapter content if available
		litReviewContent := "No literature review summary available."
		litReviewChapter, lrErr := s.store.GetChapterByProjectIDAndType(ctx, sqlc.GetChapterByProjectIDAndTypeParams{ProjectID: pgtype.UUID{Bytes: projectID, Valid: true}, Type: "literature_review"})
		if lrErr == nil && litReviewChapter.Content.Valid {
			// Create a summary of litReviewChapter.Content (can be another AI call or simple truncation)
			summaryLimit := 500 // characters
			if len(litReviewChapter.Content.String) > summaryLimit {
				litReviewContent = litReviewChapter.Content.String[:summaryLimit] + "..."
			} else {
				litReviewContent = litReviewChapter.Content.String
			}
		}
		generatedContent, err = s.aiService.GenerateIntroduction(ctx, project.Title, project.Specialization, litReviewContent)
	case "methodology":
		// For methodology, we might need research type (e.g. from project description or a dedicated field)
		researchType := "general academic research" // Placeholder, extract from project if possible
		if project.Description.Valid && strings.Contains(strings.ToLower(project.Description.String), "qualitative") {
			researchType = "Qualitative Research"
		} else if project.Description.Valid && strings.Contains(strings.ToLower(project.Description.String), "quantitative") {
			researchType = "Quantitative Research"
		}
		generatedContent, err = s.aiService.GenerateMethodologyTemplate(ctx, project.Title, project.Specialization, researchType)
	default:
		s.logger.Warn("Unsupported chapter type for AI generation", "type", chapterType)
		return sqlc.Chapter{}, fmt.Errorf("AI generation not supported for chapter type: %s", chapterType)
	}

	if err != nil {
		s.logger.Error("AI content generation failed", "chapterID", chapterID, "type", chapterType, "error", err)
		return sqlc.Chapter{}, fmt.Errorf("AI generation failed: %w", err)
	}

	// Update the chapter with generated content
	updateParams := apimodels.UpdateChapterRequest{
		Content: &generatedContent,
		Status:  models.ToStringPtr("generated"), // status defined in your api model
	}
	return s.UpdateChapter(ctx, chapterID, projectID, userID, updateParams)
}

// --- Reference Methods ---
func (s *ResearchService) CreateReference(ctx context.Context, userID uuid.UUID, req apimodels.CreateReferenceRequest) (sqlc.Reference, error) {
	s.logger.Info("Creating reference", "projectID", req.ProjectID, "title", req.Title, "userID", userID)
	// Verify user owns the project
	_, err := s.GetUserProjectByID(ctx, req.ProjectID, userID)
	if err != nil {
		s.logger.Warn("User does not own project for reference creation", "projectID", req.ProjectID, "userID", userID)
		return sqlc.Reference{}, ErrProjectNotFound
	}

	params := sqlc.CreateReferenceParams{
		ProjectID:       pgtype.UUID{Bytes: req.ProjectID, Valid: true},
		Title:           req.Title,
		Authors:         pgtype.Text{String: derefString(req.Authors), Valid: req.Authors != nil},
		Journal:         pgtype.Text{String: derefString(req.Journal), Valid: req.Journal != nil},
		PublicationYear: pgtype.Int4{Int32: int32(derefInt(req.PublicationYear)), Valid: req.PublicationYear != nil},
		Doi:             pgtype.Text{String: derefString(req.DOI), Valid: req.DOI != nil},
		Url:             pgtype.Text{String: derefString(req.URL), Valid: req.URL != nil},
		CitationApa:     pgtype.Text{String: derefString(req.CitationAPA), Valid: req.CitationAPA != nil},
		CitationMla:     pgtype.Text{String: derefString(req.CitationMLA), Valid: req.CitationMLA != nil},
	}

	ref, err := s.store.CreateReference(ctx, params)
	if err != nil {
		s.logger.Error("Failed to create reference in DB", "projectID", req.ProjectID, "error", err)
		return sqlc.Reference{}, fmt.Errorf("could not create reference: %w", err)
	}
	s.logger.Info("Reference created successfully", "referenceID", ref.ID)
	return ref, nil
}

func (s *ResearchService) GetProjectReferences(ctx context.Context, projectID, userID uuid.UUID) ([]sqlc.Reference, error) {
	s.logger.Info("Fetching references for project", "projectID", projectID, "userID", userID)
	// Verify user owns the project
	_, err := s.GetUserProjectByID(ctx, projectID, userID)
	if err != nil {
		return nil, ErrProjectNotFound
	}

	refs, err := s.store.GetReferencesByProjectID(ctx, pgtype.UUID{Bytes: projectID, Valid: true})
	if err != nil {
		s.logger.Error("Failed to get project references from DB", "projectID", projectID, "error", err)
		return nil, fmt.Errorf("database error fetching references: %w", err)
	}
	if refs == nil {
		return []sqlc.Reference{}, nil
	}
	return refs, nil
}

func (s *ResearchService) DeleteReference(ctx context.Context, referenceID, projectID, userID uuid.UUID) error {
	s.logger.Info("Deleting reference", "referenceID", referenceID, "projectID", projectID, "userID", userID)
	// Verify user owns the project the reference belongs to
	_, err := s.GetUserProjectByID(ctx, projectID, userID)
	if err != nil {
		return ErrProjectNotFound
	}

	err = s.store.DeleteReference(ctx, sqlc.DeleteReferenceParams{ID: pgtype.UUID{Bytes: referenceID, Valid: true}, ProjectID: pgtype.UUID{Bytes: projectID, Valid: true}})
	if err != nil {
		s.logger.Error("Failed to delete reference from DB", "referenceID", referenceID, "error", err)
		return fmt.Errorf("could not delete reference: %w", err)
	}
	s.logger.Info("Reference deleted successfully", "referenceID", referenceID)
	return nil
}

// Helper functions for dereferencing pointers to strings/ints
func derefString(s *string) string {
	if s != nil {
		return *s
	}
	return ""
}
func derefInt(i *int) int {
	if i != nil {
		return *i
	}
	return 0
}

// Placeholder for document generation service integration
func (s *ResearchService) GenerateDocument(ctx context.Context, projectID, userID uuid.UUID) (sqlc.GeneratedDocument, error) {
	s.logger.Info("Initiating document generation process", "projectID", projectID, "userID", userID)
	project, err := s.GetUserProjectByID(ctx, projectID, userID)
	if err != nil {
		return sqlc.GeneratedDocument{}, err
	}

	mockFileName := fmt.Sprintf("project_%s_thesis.docx", projectID.String()[:8])
	mockFilePath := fmt.Sprintf("/generated_docs/%s", mockFileName)

	docParams := sqlc.CreateGeneratedDocumentParams{
		ProjectID: pgtype.UUID{Bytes: projectID, Valid: true},
		FileName:  mockFileName,
		FilePath:  mockFilePath,
		// FileSize:  pgtype.Int8{Int64: 10240, Valid: true}, // 10KB placeholder
		// MimeType:  pgtype.Text{String: "application/vnd.openxmlformats-officedocument.wordprocessingml.document", Valid: true},
		// Status defaults to 'processing'
	}
	dbDoc, err := s.store.CreateGeneratedDocument(ctx, docParams)
	if err != nil {
		s.logger.Error("Failed to create generated document record", "projectID", projectID, "error", err)
		return sqlc.GeneratedDocument{}, fmt.Errorf("could not create document record: %w", err)
	}
	// Gather data for Python service
	chaptersDB, err := s.store.GetChaptersByProjectID(ctx, pgtype.UUID{Bytes: projectID, Valid: true})
	if err != nil {
		s.updateDocStatus(ctx, dbDoc.ID.Bytes, "failed", "Error fetching chapters")
		return dbDoc, fmt.Errorf("failed to fetch chapters for doc gen: %w", err)
	}
	var chaptersPy []PythonChapterData
	for _, ch := range chaptersDB {
		if ch.Status.String == "approved" || ch.Status.String == "generated" { // Only include approved/generated chapters
			chaptersPy = append(chaptersPy, PythonChapterData{
				Type:    ch.Type,
				Title:   ch.Title,
				Content: ch.Content.String,
			})
		}
	}

	referencesDB, err := s.store.GetReferencesByProjectID(ctx, pgtype.UUID{Bytes: projectID, Valid: true})
	if err != nil {
		s.updateDocStatus(ctx, dbDoc.ID.Bytes, "failed", "Error fetching references")
		return dbDoc, fmt.Errorf("failed to fetch references for doc gen: %w", err)
	}
	var referencesPy []PythonReferenceData
	for _, ref := range referencesDB {
		if ref.CitationApa.Valid {
			referencesPy = append(referencesPy, PythonReferenceData{CitationAPA: ref.CitationApa.String})
		}
	}

	pythonReqPayload := PythonDocGenRequest{
		ProjectID:      project.ID.Bytes,
		ResearchTitle:  project.Title,
		StudentName:    "A. User", // Get from user profile later
		UniversityName: project.University.String,
		Specialization: project.Specialization,
		Chapters:       chaptersPy,
		References:     referencesPy,
		FormattingOptions: map[string]interface{}{ // Default formatting
			"font_family":    "Times New Roman",
			"font_size_main": 12,
			"line_spacing":   1.5,
		},
	}

	jsonData, err := json.Marshal(pythonReqPayload)
	if err != nil {
		s.updateDocStatus(ctx, dbDoc.ID.Bytes, "failed", "Error marshalling request for Python service")
		return dbDoc, fmt.Errorf("failed to marshal python request: %w", err)
	}

	// Make HTTP call to Python service (synchronous for MVP simplicity)
	pythonServiceURL := "" //s.config.PythonDocGenServiceURL // Add this to your Go config
	if pythonServiceURL == "" {
		pythonServiceURL = "http://localhost:8001/generate-document" // Default for local
	}

	s.logger.Info("Calling Python document generation service", "url", pythonServiceURL)
	httpClient := &http.Client{Timeout: 2 * time.Minute} // Generous timeout for doc gen
	resp, err := httpClient.Post(pythonServiceURL, "application/json", bytes.NewBuffer(jsonData))
	if err != nil {
		s.logger.Error("Failed to call Python document generation service", "error", err)
		s.updateDocStatus(ctx, dbDoc.ID.Bytes, "failed", fmt.Sprintf("Python service call error: %v", err))
		return dbDoc, fmt.Errorf("python service call failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusAccepted && resp.StatusCode != http.StatusOK { // FastAPI might return 202 or 200
		bodyBytes, _ := io.ReadAll(resp.Body)
		errMsg := fmt.Sprintf("Python service returned error: %s, Body: %s", resp.Status, string(bodyBytes))
		s.logger.Error(errMsg)
		s.updateDocStatus(ctx, dbDoc.ID.Bytes, "failed", fmt.Sprintf("Python service error: %s", resp.Status))
		return dbDoc, fmt.Errorf(errMsg)
	}

	var pyResp PythonDocGenResponse
	if err := json.NewDecoder(resp.Body).Decode(&pyResp); err != nil {
		s.logger.Error("Failed to decode response from Python service", "error", err)
		s.updateDocStatus(ctx, dbDoc.ID.Bytes, "failed", "Python service response decode error")
		return dbDoc, fmt.Errorf("python service decode error: %w", err)
	}

	// If successful, update DB record with file name and path
	// The Python service saves to a known location pattern or a shared volume.
	// The Go service constructs the path it knows the Python service used.
	// Example: /app/generated_documents in Python container, mapped to ./data/generated_documents on host
	// Go service needs to know this host path to serve the file.
	// For Docker, the path would be relative to a shared volume.
	generatedFilePath := fmt.Sprintf("%s/%s", OUTPUT_DIR_FOR_GO, pyResp.FileName) // OUTPUT_DIR_FOR_GO is the path Go uses to access the file

	_, err = s.store.UpdateGeneratedDocument(ctx, sqlc.UpdateGeneratedDocumentParams{ // Assuming you add this query
		ID:       dbDoc.ID,
		FileName: pyResp.FileName,
		FilePath: generatedFilePath,
		Status:   pgtype.Text{String: "completed", Valid: true},
		//FileSize:  // Python could return this, or Go could stat the file
	})
	if err != nil {
		s.logger.Error("Failed to update document record to completed", "docID", dbDoc.ID, "error", err)
		// Log error but client already got a positive response from Python (potentially)
		// This part is tricky with synchronous calls. An async approach is better.
	}
	dbDoc.FileName = pyResp.FileName
	dbDoc.FilePath = generatedFilePath
	dbDoc.Status = pgtype.Text{String: "completed", Valid: true}

	s.logger.Info("Document generation request processed by Python service.", "docID", dbDoc.ID, "fileName", pyResp.FileName)
	return dbDoc, nil
}

// Helper to update document status
func (s *ResearchService) updateDocStatus(ctx context.Context, docID uuid.UUID, status string, statusMessage string) {
	// You'll need an UpdateGeneratedDocument query that can set status and a status_message field
	s.logger.Info("Updating document status", "docID", docID, "status", status, "message", statusMessage)
	_, err := s.store.UpdateGeneratedDocumentStatus(ctx, sqlc.UpdateGeneratedDocumentStatusParams{
		ID:     pgtype.UUID{Bytes: docID, Valid: true},
		Status: pgtype.Text{String: status, Valid: true},
		// Add a status_message field to your generated_documents table and sqlc query
	})
	if err != nil {
		s.logger.Error("Failed to update document status in DB", "docID", docID, "error", err)
	}
}
