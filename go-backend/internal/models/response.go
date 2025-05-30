package models

import (
	"time"

	"github.com/shawgichan/research-service/go-backend/internal/db/sqlc" // For direct use or mapping

	"github.com/google/uuid"
)

type UserResponse struct {
	ID         uuid.UUID `json:"id"`
	Email      string    `json:"email"`
	FirstName  string    `json:"first_name"`
	LastName   string    `json:"last_name"`
	IsVerified bool      `json:"is_verified"`
	CreatedAt  time.Time `json:"created_at"`
}

func ToUserResponse(user sqlc.User) UserResponse {
	return UserResponse{
		ID:         user.ID.Bytes, //tobe validated
		Email:      user.Email,
		FirstName:  user.FirstName,
		LastName:   user.LastName,
		IsVerified: user.IsVerified.Bool, // sqlc generates pgtype.Bool for NULLABLE booleans
		CreatedAt:  user.CreatedAt.Time,  // sqlc generates pgtype.Timestamptz
	}
}

type LoginUserResponse struct {
	SessionID             uuid.UUID    `json:"session_id"`
	AccessToken           string       `json:"access_token"`
	AccessTokenExpiresAt  time.Time    `json:"access_token_expires_at"`
	RefreshToken          string       `json:"refresh_token"`
	RefreshTokenExpiresAt time.Time    `json:"refresh_token_expires_at"`
	User                  UserResponse `json:"user"`
}

type ProjectResponse struct {
	ID             uuid.UUID           `json:"id"`
	UserID         uuid.UUID           `json:"user_id"`
	Title          string              `json:"title"`
	Specialization string              `json:"specialization"`
	University     string              `json:"university,omitempty"`
	Description    string              `json:"description,omitempty"`
	Status         string              `json:"status"`
	CreatedAt      time.Time           `json:"created_at"`
	UpdatedAt      time.Time           `json:"updated_at"`
	Chapters       []ChapterResponse   `json:"chapters,omitempty"`   // Optionally include chapters
	References     []ReferenceResponse `json:"references,omitempty"` // Optionally include references
}

func ToProjectResponse(project sqlc.ResearchProject) ProjectResponse {
	return ProjectResponse{
		ID:             project.ID.Bytes,     //tobe validated
		UserID:         project.UserID.Bytes, //tobe validated
		Title:          project.Title,
		Specialization: project.Specialization,
		University:     project.University.String,
		Description:    project.Description.String,
		Status:         project.Status.String,
		CreatedAt:      project.CreatedAt.Time,
		UpdatedAt:      project.UpdatedAt.Time,
	}
}

type ChapterResponse struct {
	ID        uuid.UUID `json:"id"`
	ProjectID uuid.UUID `json:"project_id"`
	Type      string    `json:"type"`
	Title     string    `json:"title"`
	Content   string    `json:"content,omitempty"` // Content might be large, consider separate endpoint for full content
	WordCount int32     `json:"word_count"`
	Status    string    `json:"status"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

func ToChapterResponse(chapter sqlc.Chapter) ChapterResponse {
	return ChapterResponse{
		ID:        chapter.ID.Bytes,        //tobe validated
		ProjectID: chapter.ProjectID.Bytes, //tobe validated
		Type:      chapter.Type,
		Title:     chapter.Title,
		Content:   chapter.Content.String,
		WordCount: chapter.WordCount.Int32,
		Status:    chapter.Status.String,
		CreatedAt: chapter.CreatedAt.Time,
		UpdatedAt: chapter.UpdatedAt.Time,
	}
}

type ReferenceResponse struct {
	ID              uuid.UUID `json:"id"`
	ProjectID       uuid.UUID `json:"project_id"`
	Title           string    `json:"title"`
	Authors         string    `json:"authors,omitempty"`
	Journal         string    `json:"journal,omitempty"`
	PublicationYear int       `json:"publication_year,omitempty"`
	DOI             string    `json:"doi,omitempty"`
	URL             string    `json:"url,omitempty"`
	CitationAPA     string    `json:"citation_apa,omitempty"`
	CitationMLA     string    `json:"citation_mla,omitempty"`
	CreatedAt       time.Time `json:"created_at"`
}

func ToReferenceResponse(ref sqlc.Reference) ReferenceResponse {
	var pubYear int
	if ref.PublicationYear.Valid {
		pubYear = int(ref.PublicationYear.Int32)
	}
	return ReferenceResponse{
		ID:              ref.ID.Bytes,        //tobe validated
		ProjectID:       ref.ProjectID.Bytes, //tobe validated
		Title:           ref.Title,
		Authors:         ref.Authors.String,
		Journal:         ref.Journal.String,
		PublicationYear: pubYear,
		DOI:             ref.Doi.String,
		URL:             ref.Url.String,
		CitationAPA:     ref.CitationApa.String,
		CitationMLA:     ref.CitationMla.String,
		CreatedAt:       ref.CreatedAt.Time,
	}
}

type GeneratedDocumentResponse struct {
	ID        uuid.UUID `json:"id"`
	ProjectID uuid.UUID `json:"project_id"`
	FileName  string    `json:"file_name"`
	FilePath  string    `json:"file_path"` // Or a download URL
	FileSize  int64     `json:"file_size"`
	MimeType  string    `json:"mime_type"`
	Status    string    `json:"status"`
	CreatedAt time.Time `json:"created_at"`
}

func ToGeneratedDocumentResponse(doc sqlc.GeneratedDocument) GeneratedDocumentResponse {
	return GeneratedDocumentResponse{
		ID:        doc.ID.Bytes,        //tobe validated
		ProjectID: doc.ProjectID.Bytes, //tobe validated
		FileName:  doc.FileName,
		FilePath:  doc.FilePath,
		FileSize:  doc.FileSize.Int64,
		MimeType:  doc.MimeType.String,
		Status:    doc.Status.String,
		CreatedAt: doc.CreatedAt.Time,
	}
}

type ErrorResponse struct {
	Error string `json:"error"`
}

type SuccessResponse struct {
	Message string      `json:"message,omitempty"`
	Data    interface{} `json:"data,omitempty"`
}
