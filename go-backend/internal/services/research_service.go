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

// Refactoring GenerateChapterContent
func (s *ResearchService) GenerateChapterContent(
	ctx context.Context,
	projectID uuid.UUID,
	chapterID uuid.UUID,
	userID uuid.UUID,
	chapterType string,
	// Pass the selected paper IDs from the request if it's a lit review
	selectedSemanticPaperIDs []string,
) (sqlc.Chapter, error) {
	s.logger.Info("Generating content for chapter", "chapterID", chapterID, "projectID", projectID, "type", chapterType, "userID", userID)
	project, err := s.GetUserProjectByID(ctx, projectID, userID)
	if err != nil {
		return sqlc.Chapter{}, err // Project not found or access denied
	}

	// Find the chapter (ensure it belongs to the project and user implicitly via project check)
	// You might want a more direct GetChapterByIDAndProjectID query here in the future.
	chapterToUpdate, err := s.store.GetChapterByID(ctx, pgtype.UUID{Bytes: chapterID, Valid: true})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) || errors.Is(err, sql.ErrNoRows) {
			s.logger.Warn("Chapter not found for content generation", "chapterID", chapterID)
			return sqlc.Chapter{}, ErrChapterNotFound
		}
		s.logger.Error("Failed to get chapter for content generation", "chapterID", chapterID, "error", err)
		return sqlc.Chapter{}, fmt.Errorf("could not retrieve chapter: %w", err)
	}

	// Verify chapter belongs to the project
	if chapterToUpdate.ProjectID.Bytes != projectID {
		s.logger.Warn("Chapter does not belong to the specified project", "chapterID", chapterID, "projectID", projectID)
		return sqlc.Chapter{}, ErrChapterNotFound // Or a more specific error
	}
	// Ensure chapter type matches (already passed as chapterType, but good to double check from DB if needed)
	if chapterToUpdate.Type != chapterType {
		s.logger.Warn("Mismatched chapter type for generation", "requestedType", chapterType, "dbType", chapterToUpdate.Type)
		return sqlc.Chapter{}, fmt.Errorf("mismatched chapter type for generation")
	}

	var generatedContent string
	// var generatedReferences []*apimodels.ReferenceResponse // This was for placeholder, now we use SemanticPaper

	switch chapterType {
	case "literature_review":
		if len(selectedSemanticPaperIDs) == 0 {
			s.logger.Warn("No semantic paper IDs provided for literature review generation", "chapterID", chapterID)
			return sqlc.Chapter{}, errors.New("at least one paper must be selected for literature review generation")
		}

		// Fetch full details of selected Semantic Scholar papers
		// This assumes selectedSemanticPaperIDs are IDs that aiService.SearchSemanticScholar can use
		// or that we have a way to fetch details by ID.
		// For simplicity now, let's assume the frontend sends enough info for SemanticPaper objects,
		// or this function fetches them.
		// A more robust way: Store SemanticPaper results from search temporarily (e.g., Redis cache keyed by user/project)
		// then retrieve the selected ones.
		// For now, let's simulate fetching/constructing SemanticPaper objects based on IDs.
		// This part needs careful implementation of how `selectedPapers` are obtained.
		// We'll assume `aiService.SearchSemanticScholar` is flexible or we have another method.
		// A better approach: the frontend does the search, displays results, user selects,
		// and then for generation, frontend sends the *full SemanticPaper objects* (or enough data to reconstruct them)
		// that were selected, not just IDs. Or, backend caches results of search and retrieves by ID.

		// Let's assume for now, for simplicity, that we re-fetch these papers based on their IDs if needed,
		// or that SearchSemanticScholar can take a list of IDs to fetch.
		// However, Semantic Scholar's search API is query-based, not ID-list based for fetching.
		// So, a better flow:
		// 1. User searches via frontend -> calls /search-papers endpoint in Go -> Go calls Semantic Scholar.
		// 2. Frontend displays results (List of SemanticPaperResponse).
		// 3. User selects papers. Frontend sends *list of selected SemanticPaperResponse objects* (or just their IDs if backend caches).
		// 4. Go backend transforms these back to `SemanticPaper` to pass to `aiService.GenerateLiteratureReview`.

		// *** Temporary/Simplified: Assume `selectedSemanticPaperIDs` map to a cached list or we re-search with a very specific query. ***
		// *** This is a placeholder for how `actualSelectedPapers` is populated. You'll need to refine this. ***
		var actualSelectedPapers []SemanticPaper
		// If you cached previous search results:
		// actualSelectedPapers = s.getCachedPapers(ctx, projectID, selectedSemanticPaperIDs)
		// Or, if the frontend sent the full selected paper details (preferred for this step):
		// The request model for GenerateChapterContentRequest would need to be updated to accept []SemanticPaper or similar.
		// For now, we'll just log a warning that this part needs to be robust.
		s.logger.Warn("Populating 'actualSelectedPapers' needs a robust implementation (e.g., from cached search results or frontend providing full selected paper data).")
		// For the demo, let's assume if selectedSemanticPaperIDs is not empty, we proceed with a dummy list if needed.
		// THIS IS A CRITICAL POINT TO IMPLEMENT CORRECTLY.
		// For now, let's just proceed assuming selectedSemanticPaperIDs can be used to get SemanticPaper objects.
		// If frontend sends full paper data:
		// `req.SelectedPapersData` would be part of `GenerateChapterContentRequest`
		// for _, paperData := range req.SelectedPapersData { actualSelectedPapers = append(actualSelectedPapers, paperData.ToAIServiceModel()) }

		// Let's simulate that 'selectedSemanticPaperIDs' are enough to identify papers for now.
		// We need a way to get `SemanticPaper` structs for these IDs.
		// This requires the frontend to send back the full `SemanticPaper` data for selected items.
		// For the purpose of this example, let's assume `selectedSemanticPaperIDs` can be used by AIService.
		// A more realistic approach if you only have IDs: fetch each paper by ID from Semantic Scholar.
		// The S2 API for paper details is like: https://api.semanticscholar.org/graph/v1/paper/{paper_id}?fields=...
		// This would mean `AIService.GenerateLiteratureReview` needs to take IDs and fetch details.
		// Or, the `ResearchService` fetches details for each ID and then passes `[]SemanticPaper` to `AIService`.

		// Let's adjust GenerateLiteratureReview to take IDs and fetch paper details.
		// This change needs to propagate to AIService.GenerateLiteratureReview as well.
		// For now, to keep current structure, let's assume the request sends full paper data.
		// So, `GenerateChapterContentRequest` should have `SelectedPapers []apimodels.SemanticPaperResponse`
		// Then in `GenerateChapterContent`:
		// var papersForAI []SemanticPaper
		// for _, pResp := range req.SelectedPapers { // req would be the parsed JSON body
		//     papersForAI = append(papersForAI, pResp.ToAIServiceModel()) // you'd need a ToAIServiceModel
		// }
		// For now, we'll pass an empty list, highlighting this dependency.
		// actualSelectedPapers = []SemanticPaper{} // Placeholder!

		if len(actualSelectedPapers) == 0 && len(selectedSemanticPaperIDs) > 0 {
			s.logger.Warn("No 'actualSelectedPapers' were populated. This indicates a missing step where selected paper details are retrieved or passed from the frontend.")
			// Attempt to fetch by ID as a fallback for this example (this is inefficient if many IDs)
			for _, paperID := range selectedSemanticPaperIDs {
				// Simulate fetching. In reality, you'd call Semantic Scholar paper details endpoint
				// This is very simplified:
				tempPaper, errFetch := s.aiService.GetSemanticPaperDetails(ctx, paperID) // You'd need to implement GetSemanticPaperDetails
				if errFetch == nil {
					actualSelectedPapers = append(actualSelectedPapers, tempPaper)
				} else {
					s.logger.Error("Could not fetch details for paper ID (simulated)", "paperID", paperID, "error", errFetch)
				}
			}
		}
		if len(actualSelectedPapers) == 0 {
			return sqlc.Chapter{}, errors.New("failed to retrieve details for selected papers")
		}

		var usedPapers []SemanticPaper
		generatedContent, usedPapers, err = s.aiService.GenerateLiteratureReview(ctx, project.Title, project.Specialization, actualSelectedPapers, 500) // 500 words per section approx
		if err != nil {
			s.logger.Error("AI literature review generation failed", "chapterID", chapterID, "error", err)
			return sqlc.Chapter{}, fmt.Errorf("AI literature review generation failed: %w", err)
		}

		// Save usedPapers as references
		for _, paper := range usedPapers {
			// Check for duplicates first
			var existingRef sqlc.Reference
			var getErr error
			if paper.DOI != nil && *paper.DOI != "" {
				existingRef, getErr = s.store.GetReferenceByDOIAndProject(ctx, sqlc.GetReferenceByDOIAndProjectParams{
					ProjectID: project.ID,
					Doi:       pgtype.Text{String: *paper.DOI, Valid: true},
				})
			} // Add similar check for SemanticScholarID if you have a query for it.

			if getErr == nil && existingRef.ID.Valid { // Found existing
				s.logger.Info("Reference already exists, skipping creation", "doi", *paper.DOI, "projectID", project.ID)
				continue
			} else if getErr != nil && !errors.Is(getErr, pgx.ErrNoRows) && !errors.Is(getErr, sql.ErrNoRows) {
				s.logger.Error("Error checking for existing reference", "projectID", project.ID, "error", getErr)
				// Continue, try to add anyway or handle error
			}

			var authorsText []string
			for _, author := range paper.Authors {
				authorsText = append(authorsText, author.Name)
			}
			var journalName string
			if paper.Journal != nil {
				journalName = paper.Journal.Name
			}
			var abstractText string
			if paper.Abstract != nil {
				abstractText = *paper.Abstract
			}
			var doiText string
			if paper.DOI != nil {
				doiText = *paper.DOI
			}

			createRefParams := sqlc.CreateReferenceParams{
				ProjectID:         project.ID,
				Title:             paper.Title,
				Authors:           pgtype.Text{String: strings.Join(authorsText, "; "), Valid: len(authorsText) > 0},
				Journal:           pgtype.Text{String: journalName, Valid: journalName != ""},
				PublicationYear:   pgtype.Int4{Int32: int32(paper.Year), Valid: paper.Year != 0},
				Doi:               pgtype.Text{String: doiText, Valid: doiText != ""},
				SemanticScholarID: pgtype.Text{String: paper.PaperID, Valid: paper.PaperID != ""},
				Abstract:          pgtype.Text{String: abstractText, Valid: abstractText != ""},
				SourceApi:         pgtype.Text{String: "semantic_scholar", Valid: true},
				// CitationAPA/MLA can be generated later or by AI if needed
			}
			_, refErr := s.store.CreateReference(ctx, createRefParams)
			if refErr != nil {
				// Handle potential unique constraint violation if duplicate check wasn't perfect
				if strings.Contains(refErr.Error(), "unique constraint") {
					s.logger.Warn("Failed to save generated reference due to unique constraint (likely duplicate)", "projectID", projectID, "title", paper.Title, "error", refErr)
				} else {
					s.logger.Error("Failed to save generated reference", "projectID", projectID, "title", paper.Title, "error", refErr)
				}
				// Continue, but log the error
			}
		}

	case "introduction":
		// Fetch lit review chapter content if available
		litReviewChapter, lrErr := s.store.GetChapterByProjectIDAndType(ctx, sqlc.GetChapterByProjectIDAndTypeParams{
			ProjectID: project.ID,
			Type:      "literature_review",
		})

		litReviewSummary := "Literature review is pending or not yet summarized."
		if lrErr == nil && litReviewChapter.Content.Valid {
			// Create a summary of litReviewChapter.Content (can be another AI call or simple truncation)
			summaryLimit := 1000 // characters for summary to pass to intro prompt
			contentStr := litReviewChapter.Content.String
			if utf8.RuneCountInString(contentStr) > summaryLimit {
				// Find a good place to cut (e.g., end of sentence)
				summaryRunes := []rune(contentStr)
				cutPoint := summaryLimit
				for i := summaryLimit; i > 0; i-- {
					if summaryRunes[i] == '.' || summaryRunes[i] == '?' || summaryRunes[i] == '!' {
						cutPoint = i + 1
						break
					}
				}
				litReviewSummary = string(summaryRunes[:cutPoint]) + "..."
			} else {
				litReviewSummary = contentStr
			}
		} else {
			s.logger.Warn("Could not retrieve literature review for introduction context", "projectID", project.ID, "error", lrErr)
		}

		// Fetch themes (this is tricky - themes aren't stored directly yet)
		// Option 1: Re-run IdentifyThemesFromAbstracts if you have the papers for the lit review
		// Option 2: Store themes alongside the lit review chapter (e.g., in a JSONB column or related table)
		// For now, let's pass an empty list of themes, highlighting this as an area for improvement.
		var keyThemesForIntro []Theme // Placeholder
		s.logger.Warn("Fetching key themes for introduction context is not fully implemented. Passing empty themes.", "projectID", project.ID)

		generatedContent, err = s.aiService.GenerateIntroduction(ctx, project.Title, project.Specialization, litReviewSummary, keyThemesForIntro)
		if err != nil {
			s.logger.Error("AI introduction generation failed", "chapterID", chapterID, "error", err)
			return sqlc.Chapter{}, fmt.Errorf("AI introduction generation failed: %w", err)
		}

	case "methodology":
		// For now, keep existing simple template generation. Phase 2 will enhance this.
		researchType := "general academic research" // Placeholder
		if project.Description.Valid {
			descLower := strings.ToLower(project.Description.String)
			if strings.Contains(descLower, "qualitative") {
				researchType = "Qualitative Research"
			}
			if strings.Contains(descLower, "quantitative") {
				researchType = "Quantitative Research"
			}
		}
		generatedContent, err = s.aiService.GenerateMethodologyTemplate(ctx, project.Title, project.Specialization, researchType)
		if err != nil {
			s.logger.Error("AI methodology template generation failed", "chapterID", chapterID, "error", err)
			return sqlc.Chapter{}, fmt.Errorf("AI methodology template generation failed: %w", err)
		}
	default:
		s.logger.Warn("Unsupported chapter type for AI generation", "type", chapterType)
		return sqlc.Chapter{}, fmt.Errorf("AI generation not supported for chapter type: %s", chapterType)
	}

	// Update the chapter with generated content
	updateParams := apimodels.UpdateChapterRequest{ // Assuming apimodels.UpdateChapterRequest is suitable
		Content: &generatedContent,
		Status:  models.ToStringPtr("generated"), // models.ToStringPtr from your existing code
	}
	// Use the existing s.UpdateChapter logic
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

// SearchPapers facilitates paper search & selection for the frontend:
func (s *ResearchService) SearchPapers(
	ctx context.Context,
	userID uuid.UUID, // To ensure user is authenticated, though project ownership might not be strict here yet
	projectID uuid.UUID, // Optional: could be used to tailor search or just for logging
	query string,
	specialization string,
	yearStart int,
	limit int,
) ([]apimodels.SemanticPaperResponse, error) { // Using your API model for response
	s.logger.Info("Searching papers via Semantic Scholar", "userID", userID, "projectID", projectID, "query", query)

	// Basic validation
	if limit <= 0 || limit > 50 { // Max limit for Semantic Scholar is typically 100, but let's cap it
		limit = 25 // Default limit
	}
	if yearStart <= 0 {
		yearStart = time.Now().Year() - 5 // Default to last 5 years
	}

	semanticPapers, err := s.aiService.SearchSemanticScholar(ctx, query, specialization, yearStart) // `limit` is handled inside your current aiService.SearchSemanticScholar
	if err != nil {
		s.logger.Error("Failed to search Semantic Scholar", "query", query, "error", err)
		return nil, fmt.Errorf("failed to search papers: %w", err)
	}

	// Transform to API response model
	responsePapers := make([]apimodels.SemanticPaperResponse, 0, len(semanticPapers))
	for _, paper := range semanticPapers {
		responsePapers = append(responsePapers, ToSemanticPaperResponse(paper)) // You'll implement this
	}

	s.logger.Info("Successfully retrieved papers from Semantic Scholar", "count", len(responsePapers))
	return responsePapers, nil
}
