package personalization

import (
	"fmt"
	"log/slog"
	"strings"

	"github.com/cheolwanpark/meows/collector/internal/db"
	"github.com/cheolwanpark/meows/collector/internal/gemini"
)

const (
	// System instruction for structured, fact-based output
	SYSTEM_INSTRUCTION = `You are a profile generator. Output must be valid JSON: {"character": "markdown content here"}

Output Structure (Markdown inside character field):
## User Profile
[2-3 sentences restating ONLY what the user explicitly wrote in their self-description. Do not infer personality traits, demographics, or background from reading behavior]

## Reading Interests
[Bullet list of specific topics/themes extracted ONLY from the article titles and content previews provided below. List actual technologies, concepts, and subjects mentioned in those articles - not general categories]

## Classification Keywords
[Single line, comma-separated, lowercase keywords derived ONLY from the user's stated interests and article topics listed above. Do not add related terms not explicitly present]

Rules:
- User Profile: Restate the user's own words, do not interpret or embellish
- Reading Interests: Extract ONLY from the specific articles shown in the prompt below
- Classification Keywords: Use ONLY terms that appear in the profile or article titles/content
- Do not invent topics, interests, or keywords based on assumptions
- Keep User Profile to 2-3 sentences maximum`

	// User prompt template with few-shot examples
	BASE_PROMPT_TEMPLATE = `Generate a structured profile based on the following information.

User's Self-Description: %s

Previous Character: %s

Recent Articles Liked:
%s

EXAMPLES OF CORRECT FORMAT:

Example 1:
Input: "Love sci-fi and fantasy. Interested in AI and machine learning."
Articles: "GPT-4 Released", "Neural Network Basics", "The Expanse Season Finale"
Output: {"character": "## User Profile\nA reader who loves science fiction and fantasy, interested in AI and machine learning.\n\n## Reading Interests\n- GPT-4\n- Neural networks\n- The Expanse (science fiction)\n\n## Classification Keywords\nsci-fi, fantasy, ai, machine learning, gpt-4, neural networks, the expanse"}

Example 2:
Input: "Software developer. Like learning about system design and Go programming."
Articles: "Kubernetes Best Practices", "Go 1.21 Features", "Microservices Architecture"
Output: {"character": "## User Profile\nA software developer who likes learning about system design and Go programming.\n\n## Reading Interests\n- Kubernetes best practices\n- Go 1.21 features and updates\n- Microservices architecture\n\n## Classification Keywords\ngo, golang, kubernetes, system design, microservices, go 1.21"}

Now generate a structured profile. Return JSON format: {"character": "your markdown here"}`

	ARTICLE_TEMPLATE = "- [%s]: %s\n"
	CONTENT_PREVIEW  = 100 // Max characters for article content preview
)

// BuildCharacterPrompt creates a prompt with progressive article addition and token checking
func (s *UpdateService) BuildCharacterPrompt(profile *db.Profile, articles []db.Article) (string, error) {
	userDesc := profile.UserDescription
	if userDesc == "" {
		userDesc = "No description provided"
	}

	oldChar := "No previous character"
	if profile.Character != nil && *profile.Character != "" {
		oldChar = *profile.Character
	}

	// Start with empty articles list
	articlesText := ""
	if len(articles) == 0 {
		articlesText = "No articles liked yet"
	} else {
		// Progressively add articles up to token limit
		var builder strings.Builder
		for i, article := range articles {
			if i >= 20 {
				// Max 20 articles as specified
				break
			}

			// Prepare content preview
			content := article.Content
			if len(content) > CONTENT_PREVIEW {
				content = content[:CONTENT_PREVIEW] + "..."
			}

			// Add article to the list
			articleLine := fmt.Sprintf(ARTICLE_TEMPLATE, article.Title, content)
			builder.WriteString(articleLine)

			// Build test prompt with current articles
			testArticlesText := builder.String()
			testPrompt := fmt.Sprintf(BASE_PROMPT_TEMPLATE, userDesc, oldChar, testArticlesText)

			// Count tokens
			tokens, err := s.gemini.CountTokens(testPrompt)
			if err != nil {
				slog.Warn("Failed to count tokens, continuing anyway", "error", err)
			} else if tokens > gemini.MAX_TOKENS {
				// Remove last article and break
				slog.Info("Token limit reached", "articles_included", i, "tokens", tokens)
				// Rebuild without the last article
				builder.Reset()
				for j := 0; j < i; j++ {
					c := articles[j].Content
					if len(c) > CONTENT_PREVIEW {
						c = c[:CONTENT_PREVIEW] + "..."
					}
					builder.WriteString(fmt.Sprintf(ARTICLE_TEMPLATE, articles[j].Title, c))
				}
				break
			}
		}
		articlesText = builder.String()
		if articlesText == "" {
			articlesText = "No articles liked yet"
		}
	}

	return fmt.Sprintf(BASE_PROMPT_TEMPLATE, userDesc, oldChar, articlesText), nil
}
