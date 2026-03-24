package extractor

import (
	"fmt"
	"strings"
)

// extractionSystemPrompt is the system prompt for fact extraction.
// It instructs GLM-4.7 to extract factual information worth remembering.
const extractionSystemPrompt = `You are a fact extraction engine. Given conversation turns, extract factual information worth remembering long-term.

Rules:
- Extract 0-5 facts. If nothing is worth remembering, return empty array [].
- Each fact must be a single, atomic statement (e.g., "User prefers dark mode", not "User talked about preferences").
- Category must be one of: person, project, preference, event, technical, general.
- Container must be one of: work, trading, clawchain, personal, general.
- Importance: 1.0 = critical identity/preference, 0.7 = useful context, 0.3 = minor detail.
- Do NOT extract greetings, small talk, or meta-conversation.
- Do NOT extract information already obvious from the conversation role (e.g., "the user asked a question").

Output format (strict JSON, no markdown):
[{"content":"...","category":"...","container":"...","importance":0.7}]`

// BuildExtractionPrompt formats conversation turns into the user prompt for GLM-4.7.
func BuildExtractionPrompt(turns []Turn) string {
	var sb strings.Builder
	sb.WriteString("Extract facts from the following conversation:\n\n")
	for _, t := range turns {
		role := strings.ToUpper(t.Role)
		sb.WriteString(fmt.Sprintf("%s: %s\n", role, t.Content))
	}
	return sb.String()
}
