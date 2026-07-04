# Implementation Plan: Auto-Fix Suggestions

Agents emit an optional machine-applyable fix alongside each finding. Fixes are posted
as native GitHub ```suggestion blocks on the review comment, so the PR author can apply
them with one click ("Commit suggestion") directly in the GitHub UI — no new write
permissions needed. The dashboard renders the same suggestion in the PR/review detail
views. A per-repo `auto_fix` toggle gates the feature.

## Background: how GitHub suggestions work

- A review comment whose body contains a fenced ` ```suggestion ` block renders a
  "Commit suggestion" button. The block's content **replaces the commented line range**.
- Single-line: the comment's `line`/`side` anchor is the replaced line.
- Multi-line: set `start_line` + `start_side` on the draft comment; the suggestion
  replaces `[start_line, line]`. `start_line < line`, same file, same hunk.
- Constraints: only lines present in the diff, `RIGHT` side only (can't suggest on
  deleted lines), and the whole range must fall within one hunk. Violating any of these
  makes `CreateReview` fail with **422 for the entire review** — which currently trips
  the fallback in `internal/github/client.go:187` that re-posts everything as a plain
  body comment. So server-side validation before posting is mandatory.

## Current pipeline (what we're extending)

```
agent prompt (internal/ai/agents/*.go)
  → agentJSON {path, line, side, body, priority}   (internal/ai/reviewer.go:56)
  → github.ReviewComment                            (internal/github/models.go:42)
  → Aggregator dedup by (path,line)                 (internal/review/aggregator.go:34)
  → PostReview → DraftReviewComment                 (internal/github/client.go:157)
  → persist models.ReviewComment                    (internal/jobs/review_job.go:405)
  → dashboard (web/app/(dashboard)/prs, reviews)
```

---

## Phase 1 — Suggestion blocks in review comments (core)

### 1.1 Data model

- `internal/github/models.go` — `ReviewComment`: add
  ```go
  StartLine  int    `json:"start_line,omitempty"` // 0 = single-line
  Suggestion string `json:"suggestion,omitempty"` // exact replacement for [StartLine|Line, Line]
  ```
- `internal/db/models/models.go` — `ReviewComment`: add `StartLine int`,
  `Suggestion string` (maps to `text`).
- New migration `internal/db/migrations/000002_add_suggestions.{up,down}.sql`:
  `ALTER TABLE review_comments ADD COLUMN start_line int NOT NULL DEFAULT 0, ADD COLUMN suggestion text NOT NULL DEFAULT '';`
  The schema-drift test (`internal/db/drift_test.go`) guards models vs SQL.

### 1.2 Agent prompts

Files: `internal/ai/agents/{code_review,security,performance,database}.go`.

Extend the JSON contract with two optional fields and emission rules:

```
"suggestion": "<exact replacement code>",   // optional
"start_line": <int>                         // optional, multi-line only
```

Rules to add to each system prompt:
- Include `suggestion` only when the complete fix fits inside the flagged line range
  and you are highly confident it is correct as-is.
- The suggestion is the literal replacement for lines `[start_line, line]` (or just
  `line` if `start_line` is omitted): preserve exact indentation, no markdown fences,
  no commentary, no placeholder text like `// ...`.
- `RIGHT` side only; every replaced line must appear in the diff hunk.
- Omit the field entirely for architectural/multi-file/uncertain fixes.

Keep the suggestion instructions in a shared `const suggestionRules` (new
`internal/ai/agents/suggestions.go`) appended to each system prompt only when the
request enables auto-fix (see 1.5) — so disabled repos pay zero prompt-token cost.

### 1.3 Parsing + merge (`internal/ai/reviewer.go`)

- Add `Suggestion string` and `StartLine int` to the `agentJSON` comment struct
  (reviewer.go:58).
- Thread both into `github.ReviewComment` in the merge loop (reviewer.go:209).

### 1.4 Validation — new `internal/ai/suggestions.go`

`ValidateSuggestions(comments []github.ReviewComment, diff []github.FileDiff) []github.ReviewComment`
runs after merge, before `computeScore`. For each comment with a suggestion, parse the
file's patch hunks (`@@ -a,b +c,d @@`) and **strip the suggestion (keep the comment)**
unless all of:

- `Side == "RIGHT"` and every line in `[StartLine|Line, Line]` exists on the new side
  of a single hunk (added or context line — not a deletion);
- `StartLine == 0 || StartLine < Line`;
- the suggestion differs from the current content of those lines (no no-op suggestions);
- size cap: suggestion ≤ 40 lines, range ≤ 20 lines (tunable consts).

Log a `suggestion_dropped` warning with the reason for observability. This is what
protects the whole review from a 422.

Note: the consensus filter and aggregator dedup key on `(path, line)` today; that key
stays unchanged — `StartLine` only affects rendering.

### 1.5 Config gate

- `internal/jobs/review_job.go:37` `repoReviewConfig`: add `AutoFix bool \`json:"auto_fix"\``.
- `internal/ai/models.go` `AnalysisRequest`: add `AutoFixEnabled bool`; set from
  `fullCfg.AutoFix` in `review_job.go:237` (and mirror in
  `internal/http/inprocess_handler.go:105`).
- `internal/ai/reviewer.go`: pass a `"suggestions_enabled"` flag through
  `mcp.Request.Context`; agents append `suggestionRules` to their system prompt when
  set. When disabled, also skip `ValidateSuggestions` and blank any suggestion fields
  a model emits anyway.
- Web: add an "Auto-fix suggestions" switch to the repo config page
  (`web/app/(dashboard)/repos/[id]`), wired to the existing repo config PUT.

### 1.6 Posting (`internal/github/client.go`)

In `PostReview` (client.go:157):

```go
body := rc.Body
if rc.Suggestion != "" {
    body += "\n\n```suggestion\n" + rc.Suggestion + "\n```"
}
dc := &github.DraftReviewComment{Path: &rc.Path, Body: &body, Line: &line, Side: &side}
if rc.StartLine > 0 {
    startSide := side
    dc.StartLine = &rc.StartLine
    dc.StartSide = &startSide
}
```

Also update `buildBodyWithComments` (the 422 fallback) to render suggestions as plain
```go-style code blocks — a body comment can't host a real suggestion block, but the
code should still be visible.

`PostSummaryComment` can note the count: "N findings include a one-click fix".

### 1.7 Persistence + API

- `review_job.go persist()` (line 432): copy `Suggestion`/`StartLine` into
  `models.ReviewComment`.
- Review/PR detail API responses already serialize `ReviewComment` rows — verify the
  handler DTOs include the new fields (add if hand-mapped).

### 1.8 Dashboard rendering

- Review detail + PR diff pages: when a comment has `suggestion`, render a
  "Suggested change" card — old lines (from the diff) vs replacement, using the
  existing `react-diff-view` styling; fall back to a plain `<pre>` block if the
  original range isn't available client-side.
- Diff annotations already anchor comments by path/line; multi-line suggestions
  anchor at `line` (the end of the range) with a "lines X–Y" label.

### 1.9 Observability + metrics

- Prometheus: `suggestions_emitted_total`, `suggestions_dropped_total{reason}`
  (in `internal/metrics`).
- Span attributes on `ai.review`: `review.suggestions`, `review.suggestions_dropped`.

### 1.10 Tests

- Table-driven unit tests for `ValidateSuggestions` — the highest-risk code: hunk
  parsing, range-in-hunk, deletion lines, multi-hunk files, no-op suggestions,
  size caps.
- `internal/ai/reviewer` parse test: agent JSON with/without suggestion fields.
- `PostReview` body-rendering test (suggestion block appended, StartLine set).
- Extend the schema-drift test run (automatic once the migration + model align).

**Estimated scope: ~1–2 days.** Ship Phase 1 alone; it's independently valuable.

---

## Phase 2 — One-click apply from the dashboard (optional)

GitHub's own "Commit suggestion" button covers the in-GitHub flow; this phase adds the
same from the PR Reviewer dashboard.

- `POST /api/reviews/comments/{id}/apply` (auth: member with repo access):
  1. Load comment + PR; refuse if stored `HeadSHA` ≠ current head (stale suggestion).
  2. Installation client: Contents API `GET` file at head → apply line replacement →
     `PUT` commit on the PR branch ("Apply pr-reviewer suggestion", author = bot).
  3. Refuse fork-branch PRs (no write access to fork).
- UI: "Apply fix" button on the suggestion card; SSE `suggestion_applied` event.
- The push triggers a `synchronize` webhook → automatic re-review validates the fix.
- New `applied_at`/`applied_by` columns on `review_comments` (or a small
  `suggestion_applications` table) for audit.

**Estimated scope: ~1 day.**

## Phase 3 — Acceptance tracking (feedback loop)

- On `synchronize`, for files with pending suggestions, fetch new content and check
  whether the suggested replacement now appears → mark `accepted`.
- Feed accepted fixes into the existing RAG `IndexFix` path (`review_job.go:161`) and
  surface an "acceptance rate per agent" panel in `/analytics`.

**Estimated scope: ~1 day.**

---

## Risks / edge cases

| Risk | Mitigation |
| --- | --- |
| One invalid suggestion 422s the whole review | `ValidateSuggestions` strips bad suggestions pre-post; fallback path keeps plain comments |
| LLM emits fenced/annotated code in `suggestion` | Prompt rules + strip leading/trailing fences in validation |
| Wrong indentation (tabs vs spaces) | Prompt rule "preserve exact indentation"; validation compares no-op only, indentation correctness is on the model — acceptance tracking (Phase 3) measures it |
| Suggestions on truncated diffs | `diff == nil` when truncated → validation drops all suggestions automatically |
| Multi-line ranges crossing hunks | Explicit single-hunk check in validation |
| Prompt-token cost on repos that don't want this | Suggestion rules only appended when `auto_fix` enabled |
