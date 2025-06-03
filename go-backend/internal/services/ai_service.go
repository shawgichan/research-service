package services

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	applogger "github.com/shawgichan/research-service/go-backend/internal/logger" // aliased
	"github.com/shawgichan/research-service/go-backend/internal/models"
)

// const openAIAPIURL = "https://api.openai.com/v1/chat/completions"
const openAIAPIURL = "https://api.groq.com/openai/v1/chat/completions"

type AIService struct {
	apiKey string
	client *http.Client
	logger *applogger.AppLogger
}

func NewAIService(apiKey string, logger *applogger.AppLogger) *AIService {
	return &AIService{
		apiKey: apiKey,
		client: &http.Client{Timeout: 60 * time.Second}, // Increased timeout for potentially long AI responses
		logger: logger,
	}
}

type Theme struct {
	Name        string
	Description string   // AI generated description of theme
	PaperIDs    []string // IDs of papers relevant to this theme
}

type SemanticPaper struct {
	PaperID string `json:"paperId"`
	Title   string `json:"title"`
	Authors []struct {
		Name string `json:"name"`
	} `json:"authors"`
	Year             int                    `json:"year"`
	Abstract         *string                `json:"abstract"` // Pointer as it can be null
	DOI              *string                `json:"doi"`
	Journal          *struct{ Name string } `json:"journal"`
	PublicationTypes []string               `json:"publicationTypes"`
	ExternalIds      map[string]string      `json:"externalIds"`
	IsOpenAccess     bool                   `json:"isOpenAccess"`
	OpenAccessPdf    *struct {
		Url string `json:"url"`
	} `json:"openAccessPdf"`
}

type semanticScholarResponse struct {
	Data []SemanticPaper `json:"data"`
	// Include total/next offset if pagination needed later
}

type OpenAIRequest struct {
	Model       string          `json:"model"`
	Messages    []OpenAIMessage `json:"messages"`
	MaxTokens   int             `json:"max_tokens,omitempty"`
	Temperature float64         `json:"temperature,omitempty"`
	// Stream bool `json:"stream,omitempty"` // For streaming responses
}

type OpenAIMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type OpenAIResponse struct {
	ID      string `json:"id"`
	Object  string `json:"object"`
	Created int64  `json:"created"`
	Model   string `json:"model"`
	Choices []struct {
		Index        int           `json:"index"`
		Message      OpenAIMessage `json:"message"`
		FinishReason string        `json:"finish_reason"`
	} `json:"choices"`
	Usage struct {
		PromptTokens     int `json:"prompt_tokens"`
		CompletionTokens int `json:"completion_tokens"`
		TotalTokens      int `json:"total_tokens"`
	} `json:"usage"`
	Error *OpenAIError `json:"error,omitempty"`
}

type OpenAIError struct {
	Message string `json:"message"`
	Type    string `json:"type"`
	Param   string `json:"param"`
	Code    string `json:"code"`
}

func (s *AIService) callOpenAI(ctx context.Context, request OpenAIRequest) (*OpenAIResponse, error) {
	jsonData, err := json.Marshal(request)
	if err != nil {
		s.logger.Error("Failed to marshal OpenAI request", "error", err)
		return nil, fmt.Errorf("failed to marshal request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, "POST", openAIAPIURL, bytes.NewBuffer(jsonData))
	if err != nil {
		s.logger.Error("Failed to create OpenAI HTTP request", "error", err)
		return nil, fmt.Errorf("failed to create http request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", fmt.Sprintf("Bearer %s", s.apiKey))

	resp, err := s.client.Do(req)
	if err != nil {
		s.logger.Error("Failed to send request to OpenAI", "error", err)
		return nil, fmt.Errorf("failed to send request to OpenAI: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		s.logger.Error("Failed to read OpenAI response body", "error", err)
		return nil, fmt.Errorf("failed to read response body: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		s.logger.Error("OpenAI API error", "status_code", resp.StatusCode, "response_body", string(body))
		// Try to unmarshal error response
		var errResp OpenAIResponse
		if json.Unmarshal(body, &errResp) == nil && errResp.Error != nil {
			return nil, fmt.Errorf("OpenAI API error: %s (type: %s, code: %s)", errResp.Error.Message, errResp.Error.Type, errResp.Error.Code)
		}
		return nil, fmt.Errorf("OpenAI API request failed with status %d: %s", resp.StatusCode, string(body))
	}

	var openAIResp OpenAIResponse
	if err := json.Unmarshal(body, &openAIResp); err != nil {
		s.logger.Error("Failed to unmarshal OpenAI response", "error", err, "response_body", string(body))
		return nil, fmt.Errorf("failed to unmarshal OpenAI response: %w", err)
	}

	if openAIResp.Error != nil {
		s.logger.Error("OpenAI API returned an error in response", "error_message", openAIResp.Error.Message)
		return nil, fmt.Errorf("OpenAI API error: %s", openAIResp.Error.Message)
	}

	if len(openAIResp.Choices) == 0 {
		s.logger.Warn("OpenAI response contained no choices")
		return nil, fmt.Errorf("no response choices from OpenAI")
	}

	return &openAIResp, nil
}

// Refactor GenerateLiteratureReview Method:
func (s *AIService) GenerateLiteratureReview(
	ctx context.Context,
	thesisTitle string,
	specialization string, // Keep for context if needed, or if Semantic Scholar search needs it directly
	selectedPapers []SemanticPaper, // Pass the actual paper objects selected by the user
	targetWordCountPerSection int,
) (string, []SemanticPaper, error) { // Return generated content AND the papers used for reference saving
	s.logger.Info("Orchestrating Literature Review generation", "thesisTitle", thesisTitle, "numSelectedPapers", len(selectedPapers))

	if len(selectedPapers) == 0 {
		s.logger.Warn("No papers selected for literature review generation", "thesisTitle", thesisTitle)
		return "No papers were selected to generate the literature review.", []SemanticPaper{}, nil // Or an error
	}

	// 1. Identify Themes from the selected papers
	themes, err := s.IdentifyThemesFromAbstracts(ctx, thesisTitle, selectedPapers)
	if err != nil {
		s.logger.Error("Failed to identify themes from abstracts", "thesisTitle", thesisTitle, "error", err)
		return "", nil, fmt.Errorf("failed to identify themes: %w", err)
	}

	if len(themes) == 0 {
		s.logger.Warn("No themes identified from abstracts. Generating a general review.", "thesisTitle", thesisTitle)
		// Fallback: Generate a general review based on all selected papers if no themes found
		// This could be a simpler prompt just asking to synthesize all provided abstracts.
		// For now, we'll proceed assuming themes are found, or handle this as an error/empty result.
		// Or, you could have a single call to GenerateLiteratureReviewSection with a generic "Overall Literature" theme.
		// Let's make a single section if no themes.
		if len(selectedPapers) > 0 {
			s.logger.Info("No specific themes found, generating a single consolidated literature review section.", "thesisTitle", thesisTitle)
			singleSectionContent, err := s.GenerateLiteratureReviewSection(ctx, thesisTitle, "Comprehensive Literature Summary", selectedPapers, targetWordCountPerSection*len(selectedPapers)/2) // Adjust word count
			if err != nil {
				s.logger.Error("Failed to generate single literature review section", "thesisTitle", thesisTitle, "error", err)
				return "", nil, fmt.Errorf("failed to generate consolidated literature review section: %w", err)
			}
			return singleSectionContent, selectedPapers, nil
		}
		return "Could not identify themes and no papers to summarize directly.", nil, errors.New("no themes identified and no papers to summarize")
	}

	s.logger.Info("Identified themes for literature review", "thesisTitle", thesisTitle, "themeCount", len(themes))

	// 2. Loop through identified themes and generate sections
	var literatureReviewContent strings.Builder
	var papersActuallyUsedInSections = make(map[string]SemanticPaper) // To avoid duplicate reference saving

	for _, theme := range themes {
		s.logger.Info("Generating section for theme", "thesisTitle", thesisTitle, "themeName", theme.Name)
		var relevantPapersForTheme []SemanticPaper
		for _, paperID := range theme.PaperIDs {
			for _, p := range selectedPapers {
				if p.PaperID == paperID {
					relevantPapersForTheme = append(relevantPapersForTheme, p)
					papersActuallyUsedInSections[p.PaperID] = p // Track papers used
					break
				}
			}
		}

		if len(relevantPapersForTheme) == 0 {
			s.logger.Warn("No relevant papers found for theme, skipping section", "themeName", theme.Name)
			continue
		}

		// Add theme heading (simple example, could be more sophisticated)
		literatureReviewContent.WriteString(fmt.Sprintf("\n## %s\n\n", theme.Name))
		if theme.Description != "" {
			literatureReviewContent.WriteString(fmt.Sprintf("%s\n\n", theme.Description))
		}

		sectionContent, err := s.GenerateLiteratureReviewSection(ctx, thesisTitle, theme.Name, relevantPapersForTheme, targetWordCountPerSection)
		if err != nil {
			s.logger.Error("Failed to generate literature review section for theme", "themeName", theme.Name, "error", err)
			// Decide: continue with other sections or fail the whole thing? For now, continue.
			literatureReviewContent.WriteString(fmt.Sprintf("[Error generating content for theme: %s]\n\n", theme.Name))
			continue
		}
		literatureReviewContent.WriteString(sectionContent)
		literatureReviewContent.WriteString("\n\n")
	}

	finalUsedPapers := make([]SemanticPaper, 0, len(papersActuallyUsedInSections))
	for _, paper := range papersActuallyUsedInSections {
		finalUsedPapers = append(finalUsedPapers, paper)
	}

	s.logger.Info("Literature Review generation complete", "thesisTitle", thesisTitle)
	return literatureReviewContent.String(), finalUsedPapers, nil
}

// Refactor GenerateIntroduction Method:
// It now takes more context from the literature review process.
func (s *AIService) GenerateIntroduction(
	ctx context.Context,
	title string,
	specialization string,
	literatureReviewSummary string, // A concise summary of the generated Lit Review
	keyThemes []Theme, // Themes identified in the Lit Review
	// researchGaps []string, // Potentially identified gaps (could be part of themes.Description or a separate AI step)
) (string, error) {
	s.logger.Info("Generating Introduction with enhanced context", "title", title)

	var themesSection strings.Builder
	if len(keyThemes) > 0 {
		themesSection.WriteString("Key themes identified in the literature include:\n")
		for _, theme := range keyThemes {
			themesSection.WriteString(fmt.Sprintf("- %s: %s\n", theme.Name, theme.Description))
		}
	} else {
		themesSection.WriteString("A comprehensive literature review was conducted.\n")
	}

	prompt := fmt.Sprintf(`
You are an academic research assistant. Generate a compelling introduction chapter (target 800-1200 words) for a research thesis.

Thesis Title: "%s"
Specialization: %s

Context from Literature Review:
%s
%s

The introduction should include these clearly demarcated sections using markdown headings (e.g., "## Background of the Study"):
1. ## Background of the Study: Briefly introduce the broader context, drawing from the literature.
2. ## Problem Statement: Clearly define the research problem or gap, informed by the literature review.
3. ## Research Questions and/or Objectives: State what the research aims to investigate or achieve. These should logically follow from the problem statement.
4. ## Significance of the Study: Explain the importance and potential contributions.
5. ## Scope and Limitations: Briefly outline the boundaries of the research. (Provide placeholders if details are unknown)
6. ## Structure of the Thesis: Briefly outline the subsequent chapters (e.g., "Chapter 2 will present a detailed literature review...").

Ensure academic tone and clarity. You may cite general concepts from the literature summary but do not invent specific paper citations unless they are explicitly provided in the literature summary.
`, title, specialization, literatureReviewSummary, themesSection.String())

	request := OpenAIRequest{
		Model: "gpt-4", // Or your preferred model
		Messages: []OpenAIMessage{
			{Role: "system", Content: "You are an expert academic writer specializing in crafting thesis introductions."},
			{Role: "user", Content: prompt},
		},
		MaxTokens:   2000, // Adjust as needed
		Temperature: 0.7,
	}

	openAIResp, err := s.callOpenAI(ctx, request)
	if err != nil {
		return "", fmt.Errorf("OpenAI API call for introduction failed: %w", err)
	}

	s.logger.Info("Introduction generated successfully with enhanced context", "title", title)
	return openAIResp.Choices[0].Message.Content, nil
}

func (s *AIService) GenerateMethodologyTemplate(ctx context.Context, title, specialization, researchType string) (string, error) {
	s.logger.Info("Generating Methodology Template", "title", title, "researchType", researchType)
	prompt := fmt.Sprintf(`
You are an academic research assistant. Generate a template for the methodology chapter (Chapter 3) of a research thesis.

Thesis Title: "%s"
Specialization: %s
Research Type/Approach: %s (e.g., Quantitative, Qualitative, Mixed-Methods, Systematic Review, Experimental Design)

The methodology template should include sections like:
1. Research Design: (Provide a placeholder based on Research Type)
2. Population and Sampling (if applicable): (Provide a placeholder)
3. Data Collection Methods/Instruments: (Provide a placeholder, suggest common methods for the research type)
4. Data Analysis Procedures: (Provide a placeholder, suggest common analysis techniques)
5. Ethical Considerations: (Provide a placeholder with common points)
6. Validity and Reliability (or Trustworthiness for qualitative): (Provide a placeholder)

Provide bracketed placeholders like [Describe specific research design here] or [Specify data analysis software, if any] for the user to fill in.
The template should be a starting point, guiding the student.
Target length: 500-800 words of guidance and placeholders.
`, title, specialization, researchType)

	request := OpenAIRequest{
		// Model: "gpt-3.5-turbo", // Can use a less powerful model for templates
		Model: "meta-llama/llama-4-scout-17b-16e-instruct",
		Messages: []OpenAIMessage{
			{Role: "system", Content: "You are an expert in research methodologies, providing structured templates."},
			{Role: "user", Content: prompt},
		},
		MaxTokens:   1500,
		Temperature: 0.5,
	}

	openAIResp, err := s.callOpenAI(ctx, request)
	if err != nil {
		return "", fmt.Errorf("OpenAI API call for methodology template failed: %w", err)
	}

	s.logger.Info("Methodology template generated successfully", "title", title)
	return openAIResp.Choices[0].Message.Content, nil
}

// extractPlaceholderReferences is a simplified placeholder.
// In a real application, you'd use more sophisticated NLP to parse references
// or have the AI return them in a structured format (e.g., JSON within the response).
func (s *AIService) extractPlaceholderReferences(content, specialization string) []*models.ReferenceResponse {
	s.logger.Info("Extracting placeholder references", "specialization", specialization)
	// This is highly simplified. A real implementation would parse the 'References' section.
	// The prompt now asks AI to demarcate the references section.
	// You would search for "---REFERENCES_START---" and "---REFERENCES_END---"
	// and then parse each line.

	// Placeholder logic:
	year := time.Now().Year() - 1
	refs := []*models.ReferenceResponse{
		{
			Title:           fmt.Sprintf("Placeholder: Key Advances in %s", specialization),
			Authors:         "Doe, J., & Smith, A.",
			Journal:         fmt.Sprintf("Journal of %s Discoveries", specialization),
			PublicationYear: year,
			DOI:             fmt.Sprintf("10.xxxx/j.%s.%d.001", specialization, year),
			CitationAPA:     fmt.Sprintf("Doe, J., & Smith, A. (%d). Placeholder: Key Advances in %s. Journal of %s Discoveries, 10(1), 1-15.", year, specialization, specialization),
		},
		{
			Title:           fmt.Sprintf("Placeholder: Contemporary Issues in %s", specialization),
			Authors:         "Lee, C.",
			Journal:         fmt.Sprintf("International %s Review", specialization),
			PublicationYear: year - 1,
			DOI:             fmt.Sprintf("10.yyyy/i.%s.%d.002", specialization, year-1),
			CitationAPA:     fmt.Sprintf("Lee, C. (%d). Placeholder: Contemporary Issues in %s. International %s Review, 5(2), 20-35.", year-1, specialization, specialization),
		},
	}
	s.logger.Info("Generated placeholder references", "count", len(refs))
	return refs
}

func (s *AIService) SearchSemanticScholar(ctx context.Context, query string, specialization string, yearStart int) ([]SemanticPaper, error) {
	const endpoint = "https://api.semanticscholar.org/graph/v1/paper/search"
	fields := "paperId,title,authors,year,abstract,doi,journal,publicationTypes,externalIds,isOpenAccess,openAccessPdf"

	// Combine query with specialization
	fullQuery := fmt.Sprintf("%s %s", query, specialization)

	// Build query params
	params := url.Values{}
	params.Set("query", fullQuery)
	params.Set("fields", fields)
	params.Set("limit", strconv.Itoa(10))
	params.Set("offset", "0") // support offset-based pagination later

	reqUrl := fmt.Sprintf("%s?%s", endpoint, params.Encode())

	// Build request
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, reqUrl, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to build request: %w", err)
	}

	// Optional: Set API key if you have one
	if s.apiKey != "" {
		req.Header.Set("x-api-key", s.apiKey)
	}

	// Make HTTP request
	resp, err := s.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to make request: %w", err)
	}
	defer resp.Body.Close()

	// Check HTTP status
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("unexpected status code: %d", resp.StatusCode)
	}

	// Decode response
	var result semanticScholarResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	// Optional: Filter by yearStart (if API doesn't support it)
	filtered := make([]SemanticPaper, 0, len(result.Data))
	for _, paper := range result.Data {
		if paper.Year >= yearStart {
			filtered = append(filtered, paper)
		}
	}

	return filtered, nil
}

func buildAbstractsSection(papers []SemanticPaper) string {
	var sb strings.Builder
	for _, paper := range papers {
		if paper.Abstract != nil {
			fmt.Fprintf(&sb, "- Paper ID: %s\n  Title: %s\n  Abstract: %s\n\n", paper.PaperID, paper.Title, *paper.Abstract)
		}
	}
	return sb.String()
}

func (s *AIService) IdentifyThemesFromAbstracts(ctx context.Context, thesisTitle string, papers []SemanticPaper) ([]Theme, error) {
	prompt := fmt.Sprintf(`You are an academic assistant.

Thesis title: "%s"

Below are abstracts of related research papers. Identify 3–5 overarching themes. For each theme, provide:
- A short name
- A 2–3 sentence description
- A list of paper IDs (from the abstracts) that belong to this theme

Return the result in this exact JSON format:
[
  {
    "Name": "Theme Name",
    "Description": "Short explanation of the theme",
    "PaperIDs": ["paperId1", "paperId2"]
  }
]

Abstracts:
%s
`, thesisTitle, buildAbstractsSection(papers))

	request := OpenAIRequest{
		Model: "gpt-4",
		Messages: []OpenAIMessage{
			{Role: "system", Content: "You are a helpful academic assistant."},
			{Role: "user", Content: prompt},
		},
		Temperature: 0.7,
	}

	resp, err := s.callOpenAI(ctx, request)
	if err != nil {
		return nil, err
	}

	raw := resp.Choices[0].Message.Content
	var themes []Theme
	if err := json.Unmarshal([]byte(raw), &themes); err != nil {
		s.logger.Warn("Failed to parse JSON from LLM", "content", raw)
		return nil, fmt.Errorf("failed to parse themes: %w", err)
	}

	return themes, nil
}

func (s *AIService) GenerateLiteratureReviewSection(ctx context.Context, thesisTitle, themeName string, relevantPapers []SemanticPaper, targetWordCount int) (string, error) {
	prompt := fmt.Sprintf(`You are writing a literature review section for a thesis titled "%s".

Theme: %s

Using the following abstracts, write an academic literature review (~%d words) discussing how these papers contribute to this theme. Use your own words and cite papers in (Author, Year) format.

Abstracts:
%s
`, thesisTitle, themeName, targetWordCount, buildAbstractsSection(relevantPapers))

	request := OpenAIRequest{
		Model: "gpt-4",
		Messages: []OpenAIMessage{
			{Role: "system", Content: "You are a skilled academic writer."},
			{Role: "user", Content: prompt},
		},
		Temperature: 0.7,
	}

	resp, err := s.callOpenAI(ctx, request)
	if err != nil {
		return "", err
	}

	return resp.Choices[0].Message.Content, nil
}

func (s *AIService) GetSemanticPaperDetails(ctx context.Context, paperID string) (SemanticPaper, error) {
	endpoint := fmt.Sprintf("https://api.semanticscholar.org/graph/v1/paper/%s", paperID)
	fields := "paperId,title,authors,year,abstract,doi,journal,publicationTypes,externalIds,isOpenAccess,openAccessPdf" // Same fields as search

	params := url.Values{}
	params.Set("fields", fields)
	reqUrl := fmt.Sprintf("%s?%s", endpoint, params.Encode())

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, reqUrl, nil)
	if err != nil {
		return SemanticPaper{}, fmt.Errorf("failed to build request for paper details: %w", err)
	}
	if s.apiKey != "" { // Assuming apiKey is for Semantic Scholar, not OpenAI
		req.Header.Set("x-api-key", s.apiKey)
	}

	resp, err := s.client.Do(req)
	if err != nil {
		return SemanticPaper{}, fmt.Errorf("failed to make request for paper details: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		bodyBytes, _ := io.ReadAll(resp.Body)
		s.logger.Error("Semantic Scholar paper details API error", "paperID", paperID, "status", resp.StatusCode, "body", string(bodyBytes))
		return SemanticPaper{}, fmt.Errorf("unexpected status code from S2 paper details API: %d - %s", resp.StatusCode, string(bodyBytes))
	}

	var paper SemanticPaper
	if err := json.NewDecoder(resp.Body).Decode(&paper); err != nil {
		return SemanticPaper{}, fmt.Errorf("failed to decode paper details response: %w", err)
	}
	return paper, nil
}

// Helper for models.Reference (since fields are pointers)
func ToStringPtr(s string) *string { return &s }
func ToIntPtr(i int) *int          { return &i }

func ToSemanticPaperResponse(paper SemanticPaper) models.SemanticPaperResponse {
	var authors []string
	for _, auth := range paper.Authors {
		authors = append(authors, auth.Name)
	}

	var abstractStr string
	if paper.Abstract != nil {
		abstractStr = *paper.Abstract
	}

	var doiStr string
	if paper.DOI != nil {
		doiStr = *paper.DOI
	}

	var journalName string
	if paper.Journal != nil {
		journalName = paper.Journal.Name
	}

	var openAccessPdfUrl *string
	if paper.OpenAccessPdf != nil && paper.OpenAccessPdf.Url != "" {
		openAccessPdfUrl = &paper.OpenAccessPdf.Url
	}

	return models.SemanticPaperResponse{
		PaperID:       paper.PaperID,
		Title:         paper.Title,
		Authors:       authors,
		Year:          paper.Year,
		Abstract:      abstractStr,
		DOI:           doiStr,
		Journal:       journalName,
		OpenAccessPDF: openAccessPdfUrl,
	}
}
