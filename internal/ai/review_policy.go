package ai

import (
	"errors"
	"strings"
	"unicode/utf8"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"github.com/Astraxx04/pr-reviewer/internal/db/models"
)

const (
	// ReviewPolicyKey is the system_configs row that stores a custom review policy.
	ReviewPolicyKey = "review_system_prompt"
	// MaxReviewPolicyRunes caps a saved policy so a settings edit cannot store an unbounded prompt.
	MaxReviewPolicyRunes = 100_000
)

// ErrReviewPolicyTooLong is returned when a saved policy exceeds MaxReviewPolicyRunes.
var ErrReviewPolicyTooLong = errors.New("review prompt exceeds 100000 characters")

// LoadReviewPolicy returns the stored policy. An empty string with a nil error
// means the row is absent and each agent keeps its built-in prompt.
func LoadReviewPolicy(db *gorm.DB) (string, error) {
	if db == nil {
		return "", nil
	}
	var row models.SystemConfig
	err := db.Where("key = ?", ReviewPolicyKey).First(&row).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return "", nil
	}
	if err != nil {
		return "", err
	}
	return row.Value, nil
}

// SaveReviewPolicy stores a custom policy. A blank value deletes the row so
// agents keep their built-in prompts.
func SaveReviewPolicy(db *gorm.DB, policy string) error {
	if db == nil {
		return errors.New("database unavailable")
	}
	policy = strings.TrimSpace(policy)
	if policy == "" {
		return db.Where("key = ?", ReviewPolicyKey).Delete(&models.SystemConfig{}).Error
	}
	if utf8.RuneCountInString(policy) > MaxReviewPolicyRunes {
		return ErrReviewPolicyTooLong
	}
	return db.Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "key"}},
		DoUpdates: clause.AssignmentColumns([]string{"value"}),
	}).Create(&models.SystemConfig{Key: ReviewPolicyKey, Value: policy}).Error
}
