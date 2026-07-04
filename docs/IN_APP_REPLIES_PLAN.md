# Scope: Reply to Review Comments from the App

Let users read and reply to bot review-comment threads directly in the PR Reviewer
dashboard, instead of having to go to GitHub. The bot already does two-way
conversations — but only on GitHub. This brings that flow into the app.

## How it works today

The bot's conversation loop is entirely GitHub-driven:

1. A review posts inline comments → tracked in `bot_comments`
   (`github_comment_id`, `path`, `line`, `body`) — `internal/jobs/review_job.go`.
2. A human replies **on GitHub** → GitHub fires a `pull_request_review_comment`
   webhook carrying `in_reply_to_id`.
3. `internal/http/webhook_handler.go:391` (`handlePRReviewComment`) matches the
   `bot_comments` row and enqueues a `ConversationJobArgs`.
4. `internal/jobs/conversation_job.go` runs the `conversation` agent and posts the
   bot's reply back to GitHub via `PostReviewCommentReply` (installation token),
   recording a `bot_replies` row.

In the app today, the PR detail page only renders a **"· replied" dot**
(`has_reply` in `internal/http/handlers/prs.go`) — it never shows the actual
thread text, and there is no reply box. Reply text lives entirely on GitHub;
`bot_replies` stores only a timestamp.

So "reply from the app" is really **two features**:
- **(a) Display** the conversation thread (the larger half — nothing renders it today).
- **(b) Post** a reply into it (small — the conversation rails already exist).

(a) is required for (b) to be usable.

## Three decisions that change the scope

### 1. Whose identity posts the reply?
- **Bot-with-attribution (recommended).** Post via the existing installation token,
  prefixing `**@user (via PR Reviewer):**`. Zero new auth; reuses
  `PostReviewCommentReply` unchanged.
- **As the actual user.** Requires storing each user's GitHub user-to-server OAuth
  token *with write scope* and posting with it. Today OAuth is login-only (read
  identity); all repo writes go through the App installation token. This is a real
  auth lift — token storage + encryption, scope upgrade, re-consent. Defer.

### 2. Where does thread content come from for display?
- **Fetch live from GitHub (recommended).** Add a client method that lists all
  review comments on the PR, group by `in_reply_to_id` into threads. Source of
  truth, no schema growth; costs one GitHub call per PR view.
- **Persist a `comment_messages` table.** Every turn stored locally. Enables
  history/analytics and offline display, but adds a table + a sync concern. Can
  layer on later.

### 3. Single-turn or multi-turn?
Today `ConversationWorker` replies **exactly once per thread** — idempotency is
keyed on the root `github_comment_id` in `bot_replies`
(`internal/jobs/conversation_job.go`). A real back-and-forth from the app needs
this relaxed to dedup **per human message** (e.g. a hash of `in_reply_to_id` +
human body, or a `last_replied_comment_id`), or the second bot reply is silently
dropped.

## Scoped changes — recommended path

Recommended path = **bot-attribution + live fetch + multi-turn**.

### Backend — GitHub client (`internal/github/client.go`)
- New `ListPRReviewComments(ctx, owner, repo, number)` returning every review
  comment on the PR (via `PullRequests.ListComments`), each with `ID`,
  `InReplyToID`, `Author`, `Body`, `Path`, `Line`, `CreatedAt`.
- Extend `ReviewCommentRef` (`internal/github/models.go`) with `InReplyToID` and
  `CreatedAt`. Add the method to the `Client` interface (and the test mock in
  `internal/http/webhook_handler_test.go`).

### Backend — new handler (`internal/http/handlers/conversations.go`)
- `GET /api/prs/{owner}/{repo}/{number}/threads` — fetch PR review comments, group
  into threads keyed by root comment, return for display. Enforce repo access
  (mirror `SuggestionHandler.Apply`).
- `POST /api/reviews/comments/{id}/reply` — body `{ "body": "..." }`. Resolve the
  `bot_comments` GitHub ID for the review comment, post the human reply to GitHub
  with attribution (installation token), then enqueue a `ConversationJobArgs` so
  the bot responds — the same rail the webhook uses.
- Wire both routes in `internal/http/router.go` and construct the handler in
  `cmd/server/main.go` (`WithTokenCache` + enqueuer + event hub, like
  `SuggestionHandler`).

### Backend — conversation worker (`internal/jobs/conversation_job.go`)
- Relax idempotency from per-thread-root to per-human-message so app-initiated
  turns beyond the first get a bot response.
- Publish a `conversation_reply` SSE event (via the event hub) when the bot answers,
  so the app can live-refresh the thread.

### Backend — access control
- Factor the repo-membership gate that `SuggestionHandler` uses into a shared
  helper (e.g. `canAccessRepoByID`) and reuse it in the threads + reply handlers.

### Frontend (`web/`)
- `lib/api.ts`: `getPRThreads()`, `postCommentReply()`, plus thread/message types.
- PR detail page
  (`web/app/(dashboard)/prs/[owner]/[repo]/[number]/page.tsx`): under each comment
  with activity, render the thread (bot original → human → bot …) and a reply
  textarea + send button. Extend the existing `useSSE` handling to refresh on the
  new `conversation_reply` event.
- Review detail page (`web/app/(dashboard)/reviews/[id]/page.tsx`): same thread
  display (optional; the PR page is the primary surface).

### Migrations
- **None** for the recommended path (live fetch). A migration appears only if you
  choose decision #2 (persist a `comment_messages` table).

## Effort

- Recommended path: **~1.5–2 days.** The reply-posting half is small (rails exist);
  the thread-*display* half — new client method, grouping, thread UI, live refresh —
  is the bulk.
- +~0.5 day for a persisted `comment_messages` table (decision #2).
- +~1–1.5 days for post-as-real-user OAuth (decision #1).

## Risks / edge cases

| Risk | Notes / mitigation |
| --- | --- |
| Attribution clarity | Replies appear under the bot's identity with a `@user` prefix — reads slightly oddly; the visible cost of skipping user-OAuth. |
| Comment ↔ GitHub-ID mapping | The existing `has_reply` logic matches `bot_comments` to review comments on **(path, line)** — fragile when two findings share one line. Reusing it for reply-targeting inherits that. Hardening: store `github_comment_id` directly on `review_comments` at post time (small change in `review_job.go`). |
| Rate / latency | Live-fetching threads adds one GitHub call per PR view. Fine at current scale; cache if it grows. |
| Reply before bot IDs are tracked | Bot comment IDs are tracked after the review posts (`GetReviewCommentsByReview` in `review_job.go`). Replies to a comment whose ID isn't tracked yet must fail gracefully. |
