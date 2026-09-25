package handlers

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"gorm.io/gorm"

	"github.com/Astraxx04/pr-reviewer/internal/ai"
)

type ReviewPromptHandler struct{ db *gorm.DB }

func NewReviewPromptHandler(db *gorm.DB) *ReviewPromptHandler {
	return &ReviewPromptHandler{db: db}
}

type reviewPromptResponse struct {
	Prompt        string `json:"prompt"`
	IsDefault     bool   `json:"is_default"`
	DefaultPrompt string `json:"default_prompt"`
}

func (h *ReviewPromptHandler) Get(w http.ResponseWriter, r *http.Request) {
	if !isAdmin(getUser(r)) {
		writeError(w, http.StatusForbidden, "admin only")
		return
	}
	stored := strings.TrimSpace(ai.LoadReviewPolicy(h.db))
	writeJSON(w, http.StatusOK, reviewPromptResponse{
		Prompt:        ai.EffectiveReviewPolicy(stored),
		IsDefault:     stored == "" || stored == strings.TrimSpace(ai.DefaultReviewPolicy),
		DefaultPrompt: ai.DefaultReviewPolicy,
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
	stored := strings.TrimSpace(ai.LoadReviewPolicy(h.db))
	writeJSON(w, http.StatusOK, reviewPromptResponse{
		Prompt:        ai.EffectiveReviewPolicy(stored),
		IsDefault:     stored == "" || stored == strings.TrimSpace(ai.DefaultReviewPolicy),
		DefaultPrompt: ai.DefaultReviewPolicy,
	})
}
