package services

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	applogger "github.com/shawgichan/research-service/go-backend/internal/logger" // aliased
	"github.com/shawgichan/research-service/go-backend/internal/models"           // For placeholder references
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

func (s *AIService) GenerateLiteratureReview(ctx context.Context, title, specialization string) (string, []*models.ReferenceResponse, error) {
	s.logger.Info("Generating Literature Review", "title", title, "specialization", specialization)
	prompt := fmt.Sprintf(`
You are an academic research assistant. Generate a comprehensive literature review for a research thesis with the following details:

Title: "%s"
Specialization: %s

Please provide:
1. A well-structured literature review (target 1500-2000 words).
2. Include at least 10-15 recent academic references (published between 2019 and the current year).
3. Organize the content with appropriate subheadings.
4. Follow academic writing standards.
5. Include in-text citations in APA format (e.g., (Author, Year)).
6. Conclude with a "References" section listing all cited works in APA format.

The literature review should cover:
- Current state of research in this area
- Key theories and frameworks relevant to the title and specialization
- Recent developments and findings
- Gaps in existing research that this thesis might address
- How this research potentially contributes to the field

Format the response as a proper academic literature review.
Ensure the "References" section is clearly demarcated at the end, like:
---REFERENCES_START---
Author, A. A. (Year). Title of work. Publisher.
Another, B. B. (Year). Title of article. Journal Title, volume(issue), pages.
---REFERENCES_END---
`, title, specialization)

	request := OpenAIRequest{
		// Model: "gpt-4-turbo-preview", // Or "gpt-3.5-turbo" for faster/cheaper, "gpt-4" for higher quality
		Model: "meta-llama/llama-4-scout-17b-16e-instruct",

		Messages: []OpenAIMessage{
			{Role: "system", Content: "You are an expert academic research assistant specializing in writing literature reviews."},
			{Role: "user", Content: prompt},
		},
		MaxTokens:   3500, // Adjust as needed
		Temperature: 0.6,  // Balance creativity and factualness
	}

	openAIResp, err := s.callOpenAI(ctx, request)
	if err != nil {
		return "", nil, fmt.Errorf("OpenAI API call failed: %w", err)
	}

	content := openAIResp.Choices[0].Message.Content

	// TODO: Implement more robust reference extraction and parsing.
	// For now, we use a placeholder.
	extractedReferences := s.extractPlaceholderReferences(content, specialization)

	s.logger.Info("Literature Review generated successfully", "title", title)
	return content, extractedReferences, nil
}

func (s *AIService) GenerateIntroduction(ctx context.Context, title, specialization, literatureReviewSummary string) (string, error) {
	s.logger.Info("Generating Introduction", "title", title, "specialization", specialization)
	prompt := fmt.Sprintf(`
You are an academic research assistant. Generate a compelling introduction chapter (target 800-1200 words) for a research thesis.

Thesis Title: "%s"
Specialization: %s
Summary of Literature Review: %s

The introduction should include:
1. Background of the study: Briefly introduce the broader context.
2. Problem statement: Clearly define the research problem or gap.
3. Research questions and/or objectives: State what the research aims to investigate or achieve.
4. Significance of the study: Explain the importance and potential contributions.
5. Scope and limitations: Briefly outline the boundaries of the research.
6. Structure of the thesis: Briefly outline the subsequent chapters.

Ensure academic tone and clarity.
`, title, specialization, literatureReviewSummary)

	request := OpenAIRequest{
		// Model: "gpt-4-turbo-preview",
		Model: "meta-llama/llama-4-scout-17b-16e-instruct",
		Messages: []OpenAIMessage{
			{Role: "system", Content: "You are an expert academic writer specializing in crafting thesis introductions."},
			{Role: "user", Content: prompt},
		},
		MaxTokens:   2000,
		Temperature: 0.7,
	}

	openAIResp, err := s.callOpenAI(ctx, request)
	if err != nil {
		return "", fmt.Errorf("OpenAI API call for introduction failed: %w", err)
	}

	s.logger.Info("Introduction generated successfully", "title", title)
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

// Helper for models.Reference (since fields are pointers)
func ToStringPtr(s string) *string { return &s }
func ToIntPtr(i int) *int          { return &i }
