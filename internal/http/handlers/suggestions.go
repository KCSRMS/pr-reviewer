package handlers

import (
	"fmt"
	"net/http"
	"strings"
	"time"

	"gorm.io/gorm"

	dbpkg "github.com/Astraxx04/pr-reviewer/internal/db"
	"github.com/Astraxx04/pr-reviewer/internal/db/models"
	"github.com/Astraxx04/pr-reviewer/internal/events"
	gh "github.com/Astraxx04/pr-reviewer/internal/github"
)

// SuggestionHandler applies an AI review comment's validated auto-fix suggestion
// directly to the PR's head branch — the dashboard's equivalent of GitHub's
// "Commit suggestion" button.
type SuggestionHandler struct {
	db            *gorm.DB
	tokenCache    *gh.InstallationTokenCache
	encryptionKey string
	eventHub      *events.Hub
}

func NewSuggestionHandler(db *gorm.DB) *SuggestionHandler {
	return &SuggestionHandler{db: db}
}

func (h *SuggestionHandler) WithTokenCache(cache *gh.InstallationTokenCache, encKey string) *SuggestionHandler {
	h.tokenCache = cache
	h.encryptionKey = encKey
	return h
}

func (h *SuggestionHandler) WithEventHub(hub *events.Hub) *SuggestionHandler {
	h.eventHub = hub
	return h
}

// Apply commits comment.Suggestion to the PR's head branch via the Contents API.
func (h *SuggestionHandler) Apply(w http.ResponseWriter, r *http.Request) {
	if h.tokenCache == nil {
		writeError(w, http.StatusServiceUnavailable, "GitHub client not configured")
		return
	}
	commentID, ok := pathID(r, "id")
	if !ok {
		writeError(w, http.StatusBadRequest, "invalid id")
		return
	}
	user := getUser(r)
	if user == nil {
		writeError(w, http.StatusUnauthorized, "not authenticated")
		return
	}

	ctx := r.Context()

	var comment models.ReviewComment
	if err := h.db.WithContext(ctx).First(&comment, commentID).Error; err != nil {
		writeError(w, http.StatusNotFound, "comment not found")
		return
	}
	if comment.Suggestion == "" {
		writeError(w, http.StatusBadRequest, "comment has no suggestion")
		return
	}
	if comment.AppliedAt != nil {
		writeError(w, http.StatusConflict, "suggestion already applied")
		return
	}

	var review models.Review
	if err := h.db.WithContext(ctx).First(&review, comment.ReviewID).Error; err != nil {
		writeError(w, http.StatusNotFound, "review not found")
		return
	}
	var pr models.PullRequest
	if err := h.db.WithContext(ctx).First(&pr, review.PRID).Error; err != nil {
		writeError(w, http.StatusNotFound, "pull request not found")
		return
	}
	var repoRow models.Repository
	if err := h.db.WithContext(ctx).First(&repoRow, pr.RepoID).Error; err != nil {
		writeError(w, http.StatusNotFound, "repository not found")
		return
	}

	if !isAdmin(user) {
		var mine int64
		h.db.WithContext(ctx).Model(&models.RepoAccess{}).
			Where("repo_id = ? AND login = ?", repoRow.ID, user.Login).Count(&mine)
		if mine == 0 {
			writeError(w, http.StatusForbidden, "no access to this repository")
			return
		}
	}

	// A newer review may have re-numbered this line or already addressed it —
	// only the latest review's suggestions are safe to apply.
	var latestReviewID uint
	h.db.WithContext(ctx).Model(&models.Review{}).
		Where("pr_id = ?", pr.ID).Order("id desc").Limit(1).Pluck("id", &latestReviewID)
	if latestReviewID != review.ID {
		writeError(w, http.StatusConflict, "a newer review exists — re-review before applying this suggestion")
		return
	}

	instClient, err := dbpkg.ResolveInstallationClient(ctx, h.db, h.encryptionKey, h.tokenCache)
	if err != nil {
		writeError(w, http.StatusBadGateway, "failed to get GitHub client: "+err.Error())
		return
	}

	livePR, err := instClient.GetPullRequest(ctx, repoRow.Owner, repoRow.Name, pr.Number)
	if err != nil {
		writeError(w, http.StatusBadGateway, "failed to fetch pull request: "+err.Error())
		return
	}
	// The suggestion's line numbers were validated against the diff at pr.HeadSHA.
	// If the branch has moved since, those line numbers are no longer trustworthy.
	if livePR.Head.Sha != pr.HeadSHA {
		writeError(w, http.StatusConflict, "the pull request has new commits since this review — re-review before applying")
		return
	}
	if livePR.Head.Repo != "" && livePR.Base.Repo != "" && livePR.Head.Repo != livePR.Base.Repo {
		writeError(w, http.StatusUnprocessableEntity, "cannot apply fixes to pull requests from forks")
		return
	}

	content, blobSHA, err := instClient.GetFileContentAtRef(ctx, repoRow.Owner, repoRow.Name, comment.Path, livePR.Head.Ref)
	if err != nil {
		writeError(w, http.StatusBadGateway, "failed to fetch file content: "+err.Error())
		return
	}

	newContent, err := applySuggestion(content, comment)
	if err != nil {
		writeError(w, http.StatusConflict, err.Error())
		return
	}

	commitSHA, err := instClient.UpdateFileContent(ctx, repoRow.Owner, repoRow.Name, comment.Path,
		livePR.Head.Ref, "Apply pr-reviewer suggestion", newContent, blobSHA)
	if err != nil {
		writeError(w, http.StatusBadGateway, "failed to commit fix: "+err.Error())
		return
	}

	now := time.Now()
	h.db.WithContext(ctx).Model(&comment).Updates(map[string]any{
		"applied_at": now,
		"applied_by": user.Login,
	})

	if h.eventHub != nil {
		h.eventHub.Publish(events.Event{
			Type: "suggestion_applied",
			Data: map[string]any{
				"comment_id": comment.ID,
				"owner":      repoRow.Owner,
				"repo":       repoRow.Name,
				"number":     pr.Number,
				"commit_sha": commitSHA,
			},
		})
	}

	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "commit_sha": commitSHA})
}

// applySuggestion replaces the 1-indexed line range [StartLine, Line] (StartLine
// defaults to Line when unset) in content with c.Suggestion. The caller must have
// already confirmed the PR's head hasn't moved since the suggestion was validated,
// so these line numbers are expected to still address the right lines.
func applySuggestion(content string, c models.ReviewComment) (string, error) {
	startLine := c.StartLine
	if startLine == 0 {
		startLine = c.Line
	}
	lines := strings.Split(content, "\n")
	if startLine < 1 || c.Line < startLine || c.Line > len(lines) {
		return "", fmt.Errorf("suggestion no longer matches the file — it may have changed")
	}

	replacement := strings.Split(c.Suggestion, "\n")
	newLines := make([]string, 0, len(lines)-(c.Line-startLine+1)+len(replacement))
	newLines = append(newLines, lines[:startLine-1]...)
	newLines = append(newLines, replacement...)
	newLines = append(newLines, lines[c.Line:]...)
	return strings.Join(newLines, "\n"), nil
}
