package api

import (
	"errors"
	"fmt"
	"net/http"
	"os" // For file download (example)

	"github.com/shawgichan/research-service/internal/api/response"
	apimodels "github.com/shawgichan/research-service/internal/models" // API request/response models
	"github.com/shawgichan/research-service/internal/services"
	"github.com/shawgichan/research-service/internal/token"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

// --- Project Handlers ---

func (s *Server) createProject(c *gin.Context) {
	authPayload := c.MustGet(authorizationPayloadKey).(*token.Payload)
	var req apimodels.CreateProjectRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		s.logger.Warn("Invalid create project request", "userID", authPayload.UserID, "error", err)
		response.BadRequest(c, "Invalid request payload", err.Error())
		return
	}

	project, err := s.researchService.CreateProject(c.Request.Context(), authPayload.UserID, req)
	if err != nil {
		s.logger.Error("Failed to create project", "userID", authPayload.UserID, "title", req.Title, "error", err)
		response.InternalServerError(c, "Failed to create project", err)
		return
	}
	response.Created(c, apimodels.ToProjectResponse(project), "Project created successfully")
}

func (s *Server) getProject(c *gin.Context) {
	authPayload := c.MustGet(authorizationPayloadKey).(*token.Payload)
	projectIDStr := c.Param("project_id")
	projectID, err := uuid.Parse(projectIDStr)
	if err != nil {
		s.logger.Warn("Invalid project ID format in getProject", "projectID", projectIDStr, "error", err)
		response.BadRequest(c, "Invalid project ID format")
		return
	}

	project, err := s.researchService.GetUserProjectByID(c.Request.Context(), projectID, authPayload.UserID)
	if err != nil {
		if errors.Is(err, services.ErrProjectNotFound) {
			s.logger.Info("Project not found or access denied for getProject", "projectID", projectID, "userID", authPayload.UserID)
			response.NotFound(c, services.ErrProjectNotFound.Error())
			return
		}
		s.logger.Error("Failed to get project", "projectID", projectID, "userID", authPayload.UserID, "error", err)
		response.InternalServerError(c, "Failed to retrieve project", err)
		return
	}

	// Optionally load chapters and references for the single project view
	chapters, err := s.researchService.GetProjectChapters(c.Request.Context(), project.ID.Bytes, authPayload.UserID)
	if err != nil {
		s.logger.Error("Failed to get chapters for project view", "projectID", project.ID, "error", err)
		// Don't fail the whole request, just log and continue without chapters
	}
	var chapterResponses []apimodels.ChapterResponse
	for _, ch := range chapters {
		chapterResponses = append(chapterResponses, apimodels.ToChapterResponse(ch))
	}

	projectResp := apimodels.ToProjectResponse(project)
	projectResp.Chapters = chapterResponses
	response.Ok(c, projectResp)
}

func (s *Server) listUserProjects(c *gin.Context) {
	authPayload := c.MustGet(authorizationPayloadKey).(*token.Payload)

	projects, err := s.researchService.GetUserProjects(c.Request.Context(), authPayload.UserID)
	if err != nil {
		s.logger.Error("Failed to list user projects", "userID", authPayload.UserID, "error", err)
		response.InternalServerError(c, "Failed to retrieve projects", err)
		return
	}

	var projectResponses []apimodels.ProjectResponse
	for _, p := range projects {
		projectResponses = append(projectResponses, apimodels.ToProjectResponse(p))
	}
	response.Ok(c, projectResponses)
}

func (s *Server) updateProject(c *gin.Context) {
	authPayload := c.MustGet(authorizationPayloadKey).(*token.Payload)
	projectIDStr := c.Param("project_id")
	projectID, err := uuid.Parse(projectIDStr)
	if err != nil {
		s.logger.Warn("Invalid project ID format in updateProject", "projectID", projectIDStr, "error", err)
		response.BadRequest(c, "Invalid project ID format")
		return
	}

	var req apimodels.UpdateProjectRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		s.logger.Warn("Invalid update project request", "projectID", projectID, "userID", authPayload.UserID, "error", err)
		response.BadRequest(c, "Invalid request payload", err.Error())
		return
	}

	updatedProject, err := s.researchService.UpdateProject(c.Request.Context(), projectID, authPayload.UserID, req)
	if err != nil {
		if errors.Is(err, services.ErrProjectNotFound) {
			s.logger.Info("Project not found or access denied for updateProject", "projectID", projectID, "userID", authPayload.UserID)
			response.NotFound(c, services.ErrProjectNotFound.Error())
			return
		}
		s.logger.Error("Failed to update project", "projectID", projectID, "userID", authPayload.UserID, "error", err)
		response.InternalServerError(c, "Failed to update project", err)
		return
	}
	response.Ok(c, apimodels.ToProjectResponse(updatedProject), "Project updated successfully")
}

func (s *Server) deleteProject(c *gin.Context) {
	authPayload := c.MustGet(authorizationPayloadKey).(*token.Payload)
	projectIDStr := c.Param("project_id")
	projectID, err := uuid.Parse(projectIDStr)
	if err != nil {
		s.logger.Warn("Invalid project ID format in deleteProject", "projectID", projectIDStr, "error", err)
		response.BadRequest(c, "Invalid project ID format")
		return
	}

	err = s.researchService.DeleteProject(c.Request.Context(), projectID, authPayload.UserID)
	if err != nil {
		if errors.Is(err, services.ErrProjectNotFound) { // If service checks and returns this
			s.logger.Info("Project not found or access denied for deleteProject", "projectID", projectID, "userID", authPayload.UserID)
			response.NotFound(c, services.ErrProjectNotFound.Error())
			return
		}
		s.logger.Error("Failed to delete project", "projectID", projectID, "userID", authPayload.UserID, "error", err)
		response.InternalServerError(c, "Failed to delete project", err)
		return
	}
	response.NoContent(c)
}

// --- Chapter Handlers (nested under projects) ---

func (s *Server) createChapter(c *gin.Context) {
	authPayload := c.MustGet(authorizationPayloadKey).(*token.Payload)
	projectIDStr := c.Param("project_id") // Get project_id from path
	projectID, err := uuid.Parse(projectIDStr)
	if err != nil {
		s.logger.Warn("Invalid project ID format in createChapter", "projectID", projectIDStr, "error", err)
		response.BadRequest(c, "Invalid project ID in path")
		return
	}

	var req apimodels.CreateChapterRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		s.logger.Warn("Invalid create chapter request", "projectID", projectID, "error", err)
		response.BadRequest(c, "Invalid request payload", err.Error())
		return
	}
	// Ensure the ProjectID in the body matches the path, or just use the path ID
	req.ProjectID = projectID

	chapter, err := s.researchService.CreateChapter(c.Request.Context(), authPayload.UserID, req)
	if err != nil {
		if errors.Is(err, services.ErrProjectNotFound) {
			response.NotFound(c, services.ErrProjectNotFound.Error())
			return
		}
		if errors.Is(err, services.ErrChapterAlreadyExists) {
			response.RespondError(c, http.StatusConflict, services.ErrChapterAlreadyExists.Error())
			return
		}
		s.logger.Error("Failed to create chapter", "projectID", req.ProjectID, "type", req.Type, "error", err)
		response.InternalServerError(c, "Failed to create chapter", err)
		return
	}
	response.Created(c, apimodels.ToChapterResponse(chapter), "Chapter created successfully")
}

func (s *Server) listProjectChapters(c *gin.Context) {
	authPayload := c.MustGet(authorizationPayloadKey).(*token.Payload)
	projectIDStr := c.Param("project_id")
	projectID, err := uuid.Parse(projectIDStr)
	if err != nil {
		s.logger.Warn("Invalid project ID format in listProjectChapters", "projectID", projectIDStr, "error", err)
		response.BadRequest(c, "Invalid project ID format")
		return
	}

	chapters, err := s.researchService.GetProjectChapters(c.Request.Context(), projectID, authPayload.UserID)
	if err != nil {
		if errors.Is(err, services.ErrProjectNotFound) {
			response.NotFound(c, services.ErrProjectNotFound.Error())
			return
		}
		s.logger.Error("Failed to list project chapters", "projectID", projectID, "userID", authPayload.UserID, "error", err)
		response.InternalServerError(c, "Failed to retrieve chapters", err)
		return
	}

	var chapterResponses []apimodels.ChapterResponse
	for _, ch := range chapters {
		chapterResponses = append(chapterResponses, apimodels.ToChapterResponse(ch))
	}
	response.Ok(c, chapterResponses)
}

func (s *Server) updateChapter(c *gin.Context) {
	authPayload := c.MustGet(authorizationPayloadKey).(*token.Payload)
	projectIDStr := c.Param("project_id")
	projectID, errP := uuid.Parse(projectIDStr)
	chapterIDStr := c.Param("chapter_id")
	chapterID, errC := uuid.Parse(chapterIDStr)

	if errP != nil || errC != nil {
		s.logger.Warn("Invalid project/chapter ID format in updateChapter", "projectID", projectIDStr, "chapterID", chapterIDStr)
		response.BadRequest(c, "Invalid project or chapter ID format")
		return
	}

	var req apimodels.UpdateChapterRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		s.logger.Warn("Invalid update chapter request", "chapterID", chapterID, "error", err)
		response.BadRequest(c, "Invalid request payload", err.Error())
		return
	}

	updatedChapter, err := s.researchService.UpdateChapter(c.Request.Context(), chapterID, projectID, authPayload.UserID, req)
	if err != nil {
		if errors.Is(err, services.ErrProjectNotFound) || errors.Is(err, services.ErrChapterNotFound) {
			s.logger.Info("Chapter/Project not found or access denied for updateChapter", "chapterID", chapterID, "projectID", projectID)
			response.NotFound(c, "Chapter or project not found, or access denied.")
			return
		}
		s.logger.Error("Failed to update chapter", "chapterID", chapterID, "error", err)
		response.InternalServerError(c, "Failed to update chapter", err)
		return
	}
	response.Ok(c, apimodels.ToChapterResponse(updatedChapter), "Chapter updated successfully")
}

func (s *Server) generateChapterContentHandler(c *gin.Context) {
	authPayload := c.MustGet(authorizationPayloadKey).(*token.Payload)
	projectIDStr := c.Param("project_id")
	projectID, errP := uuid.Parse(projectIDStr)
	chapterIDStr := c.Param("chapter_id")
	chapterID, errC := uuid.Parse(chapterIDStr)

	if errP != nil || errC != nil {
		s.logger.Warn("Invalid project/chapter ID format in generateChapterContentHandler", "projectID", projectIDStr, "chapterID", chapterIDStr)
		response.BadRequest(c, "Invalid project or chapter ID format")
		return
	}

	// We need the chapter type. The client should send it, or we fetch the chapter to get its type.
	// For this example, let's assume the client sends it in the request body.
	var reqBody struct {
		Type string `json:"type" binding:"required,oneof=introduction literature_review methodology"`
	}
	if err := c.ShouldBindJSON(&reqBody); err != nil {
		s.logger.Warn("Invalid generate chapter content request: missing type", "chapterID", chapterID, "error", err)
		response.BadRequest(c, "Chapter type is required in request body (introduction, literature_review, methodology)", err.Error())
		return
	}

	chapter, err := s.researchService.GenerateChapterContent(c.Request.Context(), projectID, chapterID, authPayload.UserID, reqBody.Type)
	if err != nil {
		if errors.Is(err, services.ErrProjectNotFound) || errors.Is(err, services.ErrChapterNotFound) {
			response.NotFound(c, "Chapter or project not found for content generation.")
			return
		}
		s.logger.Error("Failed to generate chapter content", "chapterID", chapterID, "type", reqBody.Type, "error", err)
		response.InternalServerError(c, fmt.Sprintf("Failed to generate content for %s", reqBody.Type), err)
		return
	}
	response.Ok(c, apimodels.ToChapterResponse(chapter), fmt.Sprintf("%s content generated successfully", reqBody.Type))
}

// --- Reference Handlers ---
func (s *Server) createReference(c *gin.Context) {
	authPayload := c.MustGet(authorizationPayloadKey).(*token.Payload)
	projectIDStr := c.Param("project_id")
	projectID, err := uuid.Parse(projectIDStr)
	if err != nil {
		response.BadRequest(c, "Invalid project ID in path")
		return
	}

	var req apimodels.CreateReferenceRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "Invalid request payload", err.Error())
		return
	}
	req.ProjectID = projectID // Ensure project ID from path is used

	ref, err := s.researchService.CreateReference(c.Request.Context(), authPayload.UserID, req)
	if err != nil {
		if errors.Is(err, services.ErrProjectNotFound) {
			response.NotFound(c, services.ErrProjectNotFound.Error())
			return
		}
		s.logger.Error("Failed to create reference", "projectID", req.ProjectID, "title", req.Title, "error", err)
		response.InternalServerError(c, "Failed to create reference", err)
		return
	}
	response.Created(c, apimodels.ToReferenceResponse(ref), "Reference created successfully")
}

func (s *Server) listProjectReferences(c *gin.Context) {
	authPayload := c.MustGet(authorizationPayloadKey).(*token.Payload)
	projectIDStr := c.Param("project_id")
	projectID, err := uuid.Parse(projectIDStr)
	if err != nil {
		response.BadRequest(c, "Invalid project ID format")
		return
	}

	refs, err := s.researchService.GetProjectReferences(c.Request.Context(), projectID, authPayload.UserID)
	if err != nil {
		if errors.Is(err, services.ErrProjectNotFound) {
			response.NotFound(c, services.ErrProjectNotFound.Error())
			return
		}
		s.logger.Error("Failed to list project references", "projectID", projectID, "error", err)
		response.InternalServerError(c, "Failed to retrieve references", err)
		return
	}

	var refResponses []apimodels.ReferenceResponse
	for _, r := range refs {
		refResponses = append(refResponses, apimodels.ToReferenceResponse(r))
	}
	response.Ok(c, refResponses)
}

func (s *Server) deleteReference(c *gin.Context) {
	authPayload := c.MustGet(authorizationPayloadKey).(*token.Payload)
	projectIDStr := c.Param("project_id")
	projectID, errP := uuid.Parse(projectIDStr)
	referenceIDStr := c.Param("reference_id")
	referenceID, errR := uuid.Parse(referenceIDStr)

	if errP != nil || errR != nil {
		response.BadRequest(c, "Invalid project or reference ID format")
		return
	}

	err := s.researchService.DeleteReference(c.Request.Context(), referenceID, projectID, authPayload.UserID)
	if err != nil {
		if errors.Is(err, services.ErrProjectNotFound) || errors.Is(err, services.ErrReferenceNotFound) {
			response.NotFound(c, "Project or reference not found, or access denied.")
			return
		}
		s.logger.Error("Failed to delete reference", "referenceID", referenceID, "error", err)
		response.InternalServerError(c, "Failed to delete reference", err)
		return
	}
	response.NoContent(c)
}

// --- Document Handlers ---
func (s *Server) generateDocumentHandler(c *gin.Context) {
	authPayload := c.MustGet(authorizationPayloadKey).(*token.Payload)
	projectIDStr := c.Param("project_id")
	projectID, err := uuid.Parse(projectIDStr)
	if err != nil {
		response.BadRequest(c, "Invalid project ID format")
		return
	}

	// Optional: Take parameters for document generation (template, etc.) from request body
	// var req apimodels.GenerateDocumentRequest
	// if err := c.ShouldBindJSON(&req); err != nil {
	//     response.BadRequest(c, "Invalid request payload for document generation", err.Error())
	//     return
	// }
	// req.ProjectID = projectID // Ensure project ID from path is used

	doc, err := s.researchService.GenerateDocument(c.Request.Context(), projectID, authPayload.UserID)
	if err != nil {
		if errors.Is(err, services.ErrProjectNotFound) {
			response.NotFound(c, services.ErrProjectNotFound.Error())
			return
		}
		s.logger.Error("Failed to initiate document generation", "projectID", projectID, "error", err)
		response.InternalServerError(c, "Failed to generate document", err)
		return
	}
	response.Ok(c, apimodels.ToGeneratedDocumentResponse(doc), "Document generation initiated")
}

func (s *Server) downloadDocumentHandler(c *gin.Context) {
	authPayload := c.MustGet(authorizationPayloadKey).(*token.Payload)
	projectIDStr := c.Param("project_id") // Not strictly needed if documentID is globally unique and has projectID
	projectID, errP := uuid.Parse(projectIDStr)
	_ = projectID // To avoid unused variable error
	documentIDStr := c.Param("document_id")
	documentID, errD := uuid.Parse(documentIDStr)

	if errP != nil || errD != nil {
		response.BadRequest(c, "Invalid project or document ID format")
		return
	}

	doc, err := s.researchService.GetGeneratedDocument(c.Request.Context(), documentID, authPayload.UserID)
	if err != nil {
		if errors.Is(err, services.ErrDocumentNotFound) {
			response.NotFound(c, services.ErrDocumentNotFound.Error())
			return
		}
		s.logger.Error("Failed to get document for download", "documentID", documentID, "error", err)
		response.InternalServerError(c, "Could not retrieve document", err)
		return
	}

	if doc.Status.String != "completed" { // Assuming status is pgtype.Text or sql.NullString
		response.RespondError(c, http.StatusAccepted, "Document is still processing or failed generation.")
		return
	}

	// This is a placeholder for actual file serving.
	// In a real app, doc.FilePath would point to a location in S3, local disk, etc.
	// You would then stream this file.
	// For local disk (example only, not for production without security):
	filePath := doc.FilePath // This might be an absolute path or relative to a base dir

	// Check if file exists - basic check
	if _, err := os.Stat(filePath); os.IsNotExist(err) {
		s.logger.Error("Document file not found on disk", "filePath", filePath, "documentID", doc.ID)
		response.NotFound(c, "Document file not found on server.")
		return
	}

	// Set headers for download
	c.Header("Content-Description", "File Transfer")
	c.Header("Content-Transfer-Encoding", "binary")
	c.Header("Content-Disposition", fmt.Sprintf("attachment; filename=%s", doc.FileName))
	if doc.MimeType.Valid {
		c.Header("Content-Type", doc.MimeType.String)
	} else {
		c.Header("Content-Type", "application/octet-stream") // Generic fallback
	}
	if doc.FileSize.Valid {
		c.Header("Content-Length", fmt.Sprintf("%d", doc.FileSize.Int64))
	}

	c.File(filePath)
	s.logger.Info("Document downloaded", "documentID", doc.ID, "fileName", doc.FileName)
}
