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

// LoadReviewPolicy returns the stored policy. An empty string means DefaultReviewPolicy.
func LoadReviewPolicy(db *gorm.DB) string {
	if db == nil {
		return ""
	}
	var row models.SystemConfig
	if err := db.Where("key = ?", ReviewPolicyKey).First(&row).Error; err != nil {
		return ""
	}
	return row.Value
}

// EffectiveReviewPolicy returns the stored policy, or DefaultReviewPolicy when it is blank.
func EffectiveReviewPolicy(stored string) string {
	stored = strings.TrimSpace(stored)
	if stored == "" {
		return DefaultReviewPolicy
	}
	return stored
}

// SaveReviewPolicy stores a custom policy. A blank value, or text equal to the
// built-in default, deletes the row so later default updates still apply.
func SaveReviewPolicy(db *gorm.DB, policy string) error {
	if db == nil {
		return errors.New("database unavailable")
	}
	policy = strings.TrimSpace(policy)
	if policy == "" || policy == strings.TrimSpace(DefaultReviewPolicy) {
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
