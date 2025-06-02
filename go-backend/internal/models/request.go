package models

import "github.com/google/uuid"

type RegisterUserRequest struct {
	Email     string `json:"email" binding:"required,email"`
	Password  string `json:"password" binding:"required,min=8"`
	FirstName string `json:"first_name" binding:"required"`
	LastName  string `json:"last_name" binding:"required"`
}

type LoginUserRequest struct {
	Email    string `json:"email" binding:"required,email"`
	Password string `json:"password" binding:"required"`
}

type RefreshTokenRequest struct {
	RefreshToken string `json:"refresh_token" binding:"required"`
}

type CreateProjectRequest struct {
	Title          string `json:"title" binding:"required,max=500"`
	Specialization string `json:"specialization" binding:"required,max=100"`
	University     string `json:"university,omitempty" binding:"max=200"`
	Description    string `json:"description,omitempty"`
}

type UpdateProjectRequest struct {
	Title          *string `json:"title,omitempty" binding:"omitempty,max=500"`
	Specialization *string `json:"specialization,omitempty" binding:"omitempty,max=100"`
	University     *string `json:"university,omitempty" binding:"omitempty,max=200"`
	Description    *string `json:"description,omitempty"`
	Status         *string `json:"status,omitempty" binding:"omitempty,oneof=draft in_progress completed cancelled"`
}

type CreateChapterRequest struct {
	ProjectID uuid.UUID `json:"project_id" binding:"required"`
	Type      string    `json:"type" binding:"required,oneof=introduction literature_review methodology results conclusion"`
	Title     string    `json:"title" binding:"required,max=300"`
	Content   string    `json:"content,omitempty"` // Content can be generated later
}

type UpdateChapterRequest struct {
	Title   *string `json:"title,omitempty" binding:"omitempty,max=300"`
	Content *string `json:"content,omitempty"`
	Status  *string `json:"status,omitempty" binding:"omitempty,oneof=draft generated approved rejected"`
}

type GenerateChapterContentRequest struct {
	ProjectID                uuid.UUID `json:"project_id" binding:"required"`
	ChapterID                uuid.UUID `json:"chapter_id" binding:"required"`         // Or Type if generating for first time and ID not known
	SelectedSemanticPaperIDs []string  `json:"selected_semantic_paper_ids,omitempty"` // For Literature Review
	// Add any specific params for generation, e.g., keywords, specific focus
}

type CreateReferenceRequest struct {
	ProjectID       uuid.UUID `json:"project_id" binding:"required"`
	Title           string    `json:"title" binding:"required"`
	Authors         *string   `json:"authors,omitempty"`
	Journal         *string   `json:"journal,omitempty" binding:"max=300"`
	PublicationYear *int      `json:"publication_year,omitempty"`
	DOI             *string   `json:"doi,omitempty" binding:"max=100"`
	URL             *string   `json:"url,omitempty"`
	CitationAPA     *string   `json:"citation_apa,omitempty"`
	CitationMLA     *string   `json:"citation_mla,omitempty"`
}

type GenerateDocumentRequest struct {
	ProjectID uuid.UUID `json:"project_id" binding:"required"`
	// Add other options like template, citation style if needed
}

type SearchPapersRequest struct {
	Query          string `json:"query" binding:"required"`          // Could be derived from project title or user input
	Specialization string `json:"specialization" binding:"required"` // From project
	YearStart      int    `json:"year_start,omitempty"`
	Limit          int    `json:"limit,omitempty"`
}
