package jobs

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/riverqueue/river"
	"gorm.io/gorm"

	"github.com/Astraxx04/pr-reviewer/internal/db/models"
	"github.com/Astraxx04/pr-reviewer/internal/notifications"
	"github.com/Astraxx04/pr-reviewer/pkg/logger"
)

// EmailDigestJobArgs triggers digest emails for all email notification configs whose
// configured cadence matches Period ("daily" or "weekly").
type EmailDigestJobArgs struct {
	Period string `json:"period"` // daily | weekly
}

func (EmailDigestJobArgs) Kind() string { return "email_digest" }

// EmailDigestWorker aggregates recent reviews and emails a summary to configured recipients.
// Email transport (SMTP settings + from address) is resolved per channel with an
// env-default fallback via notifications.ResolveEmail.
type EmailDigestWorker struct {
	river.WorkerDefaults[EmailDigestJobArgs]

	DB  *gorm.DB
	Log *logger.Logger
}

func (w *EmailDigestWorker) Work(ctx context.Context, job *river.Job[EmailDigestJobArgs]) error {
	period := job.Args.Period
	if period == "" {
		period = "daily"
	}
	window := 24 * time.Hour
	if period == "weekly" {
		window = 7 * 24 * time.Hour
	}
	since := time.Now().Add(-window)

	var configs []models.NotificationConfig
	if err := w.DB.WithContext(ctx).
		Where("channel = ? AND enabled = true", "email").
		Find(&configs).Error; err != nil {
		return err
	}

	sent := 0
	for _, cfg := range configs {
		var ec notifications.EmailChannelConfig
		if json.Unmarshal(cfg.Config, &ec) != nil {
			continue
		}
		if ec.Digest != period || len(ec.To) == 0 {
			continue
		}

		rows := w.aggregate(ctx, cfg.RepoID, since)
		if len(rows) == 0 {
			continue // nothing to report this period
		}

		smtp, from := notifications.ResolveEmail(ec)
		subject := fmt.Sprintf("[PR Reviewer] %s digest — %d reviews", capitalize(period), len(rows))
		if err := notifications.SendEmail(ctx, smtp, from, ec.To, subject, notifications.RenderDigest(period, since, toDigestEntries(rows))); err != nil {
			w.Log.Error("digest email failed", "config_id", cfg.ID, "error", err)
			continue
		}
		sent++
	}
	if sent > 0 {
		w.Log.Info("digest emails sent", "period", period, "count", sent)
	}
	return nil
}

// digestRow is one review summarised for the digest.
type digestRow struct {
	Owner     string
	RepoName  string
	PRNumber  int
	Title     string
	Status    string
	Score     int
	CreatedAt time.Time
}

func (w *EmailDigestWorker) aggregate(ctx context.Context, repoID *uint, since time.Time) []digestRow {
	tx := w.DB.WithContext(ctx).
		Table("reviews").
		Select(`repositories.owner, repositories.name as repo_name, pull_requests.number as pr_number,
			pull_requests.title, reviews.status, reviews.score, reviews.created_at`).
		Joins("JOIN pull_requests ON pull_requests.id = reviews.pr_id").
		Joins("JOIN repositories ON repositories.id = pull_requests.repo_id").
		Where("reviews.created_at >= ?", since).
		Order("reviews.created_at desc")
	if repoID != nil {
		tx = tx.Where("repositories.id = ?", *repoID)
	}
	var rows []digestRow
	_ = tx.Scan(&rows).Error
	return rows
}

// toDigestEntries maps the aggregated rows to the notifications digest model,
// which owns the branded HTML rendering.
func toDigestEntries(rows []digestRow) []notifications.DigestEntry {
	entries := make([]notifications.DigestEntry, 0, len(rows))
	for _, r := range rows {
		entries = append(entries, notifications.DigestEntry{
			Owner:    r.Owner,
			RepoName: r.RepoName,
			PRNumber: r.PRNumber,
			Title:    r.Title,
			Status:   r.Status,
			Score:    r.Score,
		})
	}
	return entries
}

func capitalize(s string) string {
	if s == "" {
		return s
	}
	return strings.ToUpper(s[:1]) + s[1:]
}
