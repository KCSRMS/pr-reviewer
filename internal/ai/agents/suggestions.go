package agents

// suggestionRules extends an agent's JSON comment schema with two optional
// fields so the model can propose an exact, one-click-applicable fix. It is
// only appended to the system prompt when the repo has auto-fix enabled —
// omitting it otherwise keeps prompt cost at zero for repos that don't want it.
const suggestionRules = `

When you are highly confident in an exact, self-contained fix for a comment, add it:
{
  ...
  "suggestion": "<exact replacement code for the flagged line(s)>",
  "start_line": <int, optional — omit for a single-line fix>
}

Suggestion rules:
- Only include "suggestion" when the complete fix fits within the flagged line range
  and you are highly confident it is correct as written — no partial fixes, no
  placeholders like "// ...".
- "suggestion" is the literal text that replaces lines [start_line, line] (or just
  line if start_line is omitted) — preserve exact indentation, no markdown fences,
  no commentary, no line numbers.
- The replaced range must be entirely within the diff you were given, on the RIGHT
  side (never suggest replacing a deleted line), and within a single diff hunk.
- Omit "suggestion" entirely for architectural changes, multi-file fixes, or
  anything you are not fully confident about.`

// suggestionsEnabled reports whether the caller asked this agent to propose
// auto-fix suggestions, via mcp.Request.Context["suggestions_enabled"].
func suggestionsEnabled(reqCtx map[string]any) bool {
	v, _ := reqCtx["suggestions_enabled"].(bool)
	return v
}

// withSuggestionRules appends suggestionRules to a base system prompt when enabled.
func withSuggestionRules(systemPrompt string, reqCtx map[string]any) string {
	if suggestionsEnabled(reqCtx) {
		return systemPrompt + suggestionRules
	}
	return systemPrompt
}
