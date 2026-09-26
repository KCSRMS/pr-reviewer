package handlers

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"unicode/utf8"

	"gorm.io/gorm"

	"github.com/Astraxx04/pr-reviewer/internal/ai"
	"github.com/Astraxx04/pr-reviewer/internal/audit"
)

type ReviewPromptHandler struct{ db *gorm.DB }

func NewReviewPromptHandler(db *gorm.DB) *ReviewPromptHandler {
	return &ReviewPromptHandler{db: db}
}

type reviewPromptResponse struct {
	Prompt    string `json:"prompt"`
	IsDefault bool   `json:"is_default"`
}

func (h *ReviewPromptHandler) Get(w http.ResponseWriter, r *http.Request) {
	if !isAdmin(getUser(r)) {
		writeError(w, http.StatusForbidden, "admin only")
		return
	}
	stored, err := ai.LoadReviewPolicy(h.db)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load review prompt")
		return
	}
	stored = strings.TrimSpace(stored)
	writeJSON(w, http.StatusOK, reviewPromptResponse{
		Prompt:    stored,
		IsDefault: stored == "",
	})
}

func (h *ReviewPromptHandler) Put(w http.ResponseWriter, r *http.Request) {
	if !isAdmin(getUser(r)) {
		writeError(w, http.StatusForbidden, "admin only")
		return
	}
	var body struct {
		Prompt string `json:"prompt"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid body")
		return
	}
	if err := ai.SaveReviewPolicy(h.db, body.Prompt); err != nil {
		if errors.Is(err, ai.ErrReviewPolicyTooLong) {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		writeError(w, http.StatusInternalServerError, "failed to save review prompt")
		return
	}
	user := getUser(r)
	trimmed := strings.TrimSpace(body.Prompt)
	if trimmed == "" {
		audit.Log(h.db, r, user.Login, user.ID, "config.review_prompt_cleared", "config", ai.ReviewPolicyKey, nil, nil)
	} else {
		sum := sha256.Sum256([]byte(trimmed))
		audit.Log(h.db, r, user.Login, user.ID, "config.review_prompt_updated", "config", ai.ReviewPolicyKey, nil, map[string]any{
			"sha256": hex.EncodeToString(sum[:]),
			"runes":  utf8.RuneCountInString(trimmed),
		})
	}
	stored, err := ai.LoadReviewPolicy(h.db)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load review prompt")
		return
	}
	stored = strings.TrimSpace(stored)
	writeJSON(w, http.StatusOK, reviewPromptResponse{
		Prompt:    stored,
		IsDefault: stored == "",
	})
}
