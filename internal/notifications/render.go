package notifications

import (
	"bytes"
	"embed"
	"fmt"
	"html/template"
	"log/slog"
	"sort"
	"strings"
	"sync"
	"time"
)

// templateFS embeds the HTML email templates. Each content template renders an
// inner fragment which is then wrapped in base.html to produce a fully branded,
// standalone email (logo header + content card + footer).
//
//go:embed templates/*.html
var templateFS embed.FS

// appName is the product name shown across all emails. It is a fixed constant —
// the single source of truth referenced by templates via the {{appName}} func.
const appName = "PR Reviewer"

var emailTemplates = template.Must(
	template.New("email").
		Funcs(template.FuncMap{"appName": func() string { return appName }}).
		ParseFS(templateFS, "templates/*.html"),
)

// Brand holds the organisation branding applied to every outgoing email. It is
// set once at startup via ConfigureBrand; the zero value falls back to sensible
// defaults so emails still render if configuration is skipped.
type Brand struct {
	LogoURL      string // absolute URL to the header logo; empty renders a text wordmark
	AppURL       string // absolute base URL of the app, used for header/footer links
	SupportEmail string // shown in the footer; omitted when empty
}

var (
	brandMu sync.RWMutex
	brand   Brand
)

// ConfigureBrand sets the branding used across all emails. Only non-empty fields
// overwrite existing values, so callers can configure incrementally. It is safe
// to call from startup wiring. When AppURL is set and LogoURL is not, the logo
// defaults to the horizontal logo served by the frontend.
func ConfigureBrand(b Brand) {
	brandMu.Lock()
	defer brandMu.Unlock()
	if b.AppURL != "" {
		brand.AppURL = strings.TrimRight(b.AppURL, "/")
	}
	if b.LogoURL != "" {
		brand.LogoURL = b.LogoURL
	} else if b.AppURL != "" && brand.LogoURL == "" {
		brand.LogoURL = brand.AppURL + "/logo-horizontal.png"
	}
	if b.SupportEmail != "" {
		brand.SupportEmail = b.SupportEmail
	}
}

// currentBrand returns a copy of the active brand under a read lock.
func currentBrand() Brand {
	brandMu.RLock()
	defer brandMu.RUnlock()
	return brand
}

// baseData is the model passed to base.html. LogoURL is typed template.URL so
// the operator-configured logo (an env value, not user input) bypasses
// html/template's URL sanitiser — this keeps https logos working and also
// permits data: URIs. It shadows the embedded Brand.LogoURL string field.
type baseData struct {
	Brand
	LogoURL   template.URL
	Year      int
	Title     string
	Preheader string
	Content   template.HTML
}

// wrap renders content into the branded base layout. Title becomes the document
// <title>; preheader is the inbox preview text.
func wrap(title, preheader string, content template.HTML) string {
	b := currentBrand()
	var buf bytes.Buffer
	data := baseData{
		Brand:     b,
		LogoURL:   template.URL(b.LogoURL),
		Year:      time.Now().Year(),
		Title:     title,
		Preheader: preheader,
		Content:   content,
	}
	if err := emailTemplates.ExecuteTemplate(&buf, "base.html", data); err != nil {
		slog.Error("email base template render failed", "error", err)
		// Fall back to the raw content so the email is still deliverable.
		return string(content)
	}
	return buf.String()
}

// renderContent executes a named content template and wraps it in the base
// layout. On failure it returns an empty string; callers should guard sends.
func renderContent(name, title, preheader string, data any) string {
	var c bytes.Buffer
	if err := emailTemplates.ExecuteTemplate(&c, name, data); err != nil {
		slog.Error("email content template render failed", "template", name, "error", err)
		return ""
	}
	return wrap(title, preheader, template.HTML(c.String()))
}

// WrapCustom wraps admin-provided HTML (a user-configured channel template whose
// {{placeholders}} have already been substituted) in the branded base layout so
// custom bodies still carry the standard header and footer. The inner HTML is
// trusted admin configuration and is injected verbatim.
func WrapCustom(title, preheader, innerHTML string) string {
	return wrap(title, preheader, template.HTML(innerHTML))
}

// --- Assignment -------------------------------------------------------------

type assignmentData struct {
	Assignee string
	PRTitle  string
	PRURL    string
	Summary  string
}

// RenderAssignment builds the "review requested" email.
func RenderAssignment(assignee, prTitle, prURL, summary string) string {
	return renderContent("assignment.html",
		fmt.Sprintf("Review requested: %s", prTitle),
		fmt.Sprintf("You've been asked to review %s", prTitle),
		assignmentData{Assignee: assignee, PRTitle: prTitle, PRURL: prURL, Summary: summary})
}

// --- Review complete --------------------------------------------------------

type reviewCompleteData struct {
	PRTitle    string
	PRURL      string
	Summary    string
	Score      int
	ScoreStyle template.CSS
	IsReReview bool
}

// RenderReviewComplete builds the "review complete" email.
func RenderReviewComplete(prTitle, prURL, summary string, score int, isReReview bool) string {
	return renderContent("review_complete.html",
		fmt.Sprintf("Review complete: %s — %d/100", prTitle, score),
		fmt.Sprintf("%s scored %d/100", prTitle, score),
		reviewCompleteData{
			PRTitle: prTitle, PRURL: prURL, Summary: summary,
			Score: score, ScoreStyle: scoreStyle(score), IsReReview: isReReview,
		})
}

// scoreStyle returns inline CSS colouring the score pill by band.
func scoreStyle(score int) template.CSS {
	switch {
	case score >= 80:
		return "background-color:#dcfce7;color:#15803d;"
	case score >= 50:
		return "background-color:#fef9c3;color:#a16207;"
	default:
		return "background-color:#fee2e2;color:#b91c1c;"
	}
}

// --- Invite -----------------------------------------------------------------

type inviteData struct {
	InvitedBy string
	Role      string
	Link      string
	Expires   string
}

// RenderInvite builds the team-invitation email.
func RenderInvite(invitedBy, role, link string, expiresAt time.Time) string {
	return renderContent("invite.html",
		fmt.Sprintf("%s invited you to %s", invitedBy, appName),
		fmt.Sprintf("Join %s as a %s", appName, role),
		inviteData{
			InvitedBy: invitedBy, Role: role, Link: link,
			Expires: expiresAt.UTC().Format("2 Jan 2006"),
		})
}

// --- Test -------------------------------------------------------------------

// RenderTest builds the "test notification" email.
func RenderTest() string {
	return renderContent("test.html",
		fmt.Sprintf("%s — test notification", appName),
		"Your email channel is configured correctly",
		nil)
}

// --- Digest -----------------------------------------------------------------

// DigestEntry is one review summarised for the digest email.
type DigestEntry struct {
	Owner    string
	RepoName string
	PRNumber int
	Title    string
	Status   string
	Score    int
}

// digestEntryView decorates a DigestEntry with presentation fields.
type digestEntryView struct {
	DigestEntry
	StatusLabel string
	StatusStyle template.CSS
}

type repoCount struct {
	Name  string
	Count int
}

type digestData struct {
	PeriodTitle string
	SinceLabel  string
	Count       int
	AvgScore    string
	Approvals   int
	Changes     int
	ByRepo      []repoCount
	Entries     []digestEntryView
	Truncated   bool
	ShownCount  int
}

const digestRowLimit = 30

// RenderDigest builds the periodic digest email from the aggregated reviews.
func RenderDigest(period string, since time.Time, entries []DigestEntry) string {
	var total, approvals, changes int
	perRepo := map[string]int{}
	for _, e := range entries {
		total += e.Score
		switch e.Status {
		case "APPROVE":
			approvals++
		case "REQUEST_CHANGES":
			changes++
		}
		perRepo[e.Owner+"/"+e.RepoName]++
	}
	avg := "0.0"
	if len(entries) > 0 {
		avg = fmt.Sprintf("%.1f", float64(total)/float64(len(entries)))
	}

	repos := make([]repoCount, 0, len(perRepo))
	for name, count := range perRepo {
		repos = append(repos, repoCount{Name: name, Count: count})
	}
	sort.Slice(repos, func(i, j int) bool {
		if repos[i].Count != repos[j].Count {
			return repos[i].Count > repos[j].Count
		}
		return repos[i].Name < repos[j].Name
	})

	shown := entries
	truncated := false
	if len(shown) > digestRowLimit {
		shown = shown[:digestRowLimit]
		truncated = true
	}
	views := make([]digestEntryView, 0, len(shown))
	for _, e := range shown {
		label, style := statusPresentation(e.Status)
		views = append(views, digestEntryView{DigestEntry: e, StatusLabel: label, StatusStyle: style})
	}

	title := capitalize(period)
	return renderContent("digest.html",
		fmt.Sprintf("%s %s digest — %d reviews", appName, period, len(entries)),
		fmt.Sprintf("%d reviews · avg %s/100", len(entries), avg),
		digestData{
			PeriodTitle: title,
			SinceLabel:  since.Format("2 Jan 2006, 15:04"),
			Count:       len(entries),
			AvgScore:    avg,
			Approvals:   approvals,
			Changes:     changes,
			ByRepo:      repos,
			Entries:     views,
			Truncated:   truncated,
			ShownCount:  len(views),
		})
}

// statusPresentation maps a review status to a human label and pill style.
func statusPresentation(status string) (string, template.CSS) {
	switch status {
	case "APPROVE":
		return "Approved", "display:inline-block;padding:2px 10px;border-radius:999px;background-color:#dcfce7;color:#15803d;font-weight:600;"
	case "REQUEST_CHANGES":
		return "Changes", "display:inline-block;padding:2px 10px;border-radius:999px;background-color:#fee2e2;color:#b91c1c;font-weight:600;"
	case "COMMENT":
		return "Comment", "display:inline-block;padding:2px 10px;border-radius:999px;background-color:#f4f4f5;color:#52525b;font-weight:600;"
	default:
		label := status
		if label == "" {
			label = "—"
		}
		return label, "display:inline-block;padding:2px 10px;border-radius:999px;background-color:#f4f4f5;color:#52525b;font-weight:600;"
	}
}

func capitalize(s string) string {
	if s == "" {
		return s
	}
	return strings.ToUpper(s[:1]) + s[1:]
}
