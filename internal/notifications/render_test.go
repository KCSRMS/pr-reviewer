package notifications

import (
	"strings"
	"testing"
	"time"
)

func TestRenderEmails(t *testing.T) {
	ConfigureBrand(Brand{
		AppURL:       "https://app.example.com",
		SupportEmail: "help@example.com",
	})

	since := time.Date(2026, time.July, 1, 9, 0, 0, 0, time.UTC)
	expires := time.Date(2026, time.July, 12, 9, 0, 0, 0, time.UTC)

	cases := []struct {
		name     string
		html     string
		contains []string
	}{
		{
			name:     "assignment",
			html:     RenderAssignment("octocat", "Add rate limiting", "https://github.com/x/y/pull/1", "Introduces a token-bucket limiter."),
			contains: []string{"Review requested", "octocat", "Add rate limiting", "View pull request"},
		},
		{
			name:     "review_complete",
			html:     RenderReviewComplete("Add rate limiting", "https://github.com/x/y/pull/1", "Looks solid overall.", 87, false),
			contains: []string{"Review complete", "87/100", "Looks solid overall.", "View full review"},
		},
		{
			name:     "invite",
			html:     RenderInvite("alice", "reviewer", "https://app.example.com/accept-invite?token=abc", expires),
			contains: []string{"invited you", "alice", "reviewer", "Accept invitation", "12 Jul 2026"},
		},
		{
			name:     "test",
			html:     RenderTest(),
			contains: []string{"Test notification", "configured correctly"},
		},
		{
			name: "digest",
			html: RenderDigest("weekly", since, []DigestEntry{
				{Owner: "acme", RepoName: "api", PRNumber: 42, Title: "Fix <script> escaping", Status: "APPROVE", Score: 91},
				{Owner: "acme", RepoName: "web", PRNumber: 7, Title: "Refactor auth", Status: "REQUEST_CHANGES", Score: 44},
			}),
			contains: []string{"Weekly digest", "Approved", "Changes", "acme/api#42", "Fix &lt;script&gt; escaping"},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if tc.html == "" {
				t.Fatalf("%s: rendered empty HTML", tc.name)
			}
			// Every email must carry the branded shell.
			for _, want := range []string{"<!DOCTYPE html>", "PR Reviewer", "help@example.com", "&copy;"} {
				if !strings.Contains(tc.html, want) {
					t.Errorf("%s: missing base-layout marker %q", tc.name, want)
				}
			}
			for _, want := range tc.contains {
				if !strings.Contains(tc.html, want) {
					t.Errorf("%s: missing content %q", tc.name, want)
				}
			}
		})
	}
}

func TestWrapCustomInjectsBranding(t *testing.T) {
	ConfigureBrand(Brand{AppURL: "https://app.example.com"})
	out := WrapCustom("Subject", "preview", "<p>Custom <strong>body</strong></p>")
	for _, want := range []string{"<!DOCTYPE html>", "<p>Custom <strong>body</strong></p>", "PR Reviewer"} {
		if !strings.Contains(out, want) {
			t.Errorf("WrapCustom missing %q", want)
		}
	}
}
