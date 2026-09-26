package github

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"

	gogithub "github.com/google/go-github/v69/github"
)

func TestListReviewCommentsStopsAtNewestLimit(t *testing.T) {
	pages := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		pages++
		q := r.URL.Query()
		if q.Get("sort") != "created" || q.Get("direction") != "desc" {
			t.Errorf("sort=%s direction=%s", q.Get("sort"), q.Get("direction"))
		}
		if pages > 1 {
			t.Error("requested another page after the limit was reached")
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Link", `<http://example.test?page=2>; rel="next"`)
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `[`)
		for i := 1; i <= 40; i++ {
			if i > 1 {
				fmt.Fprint(w, ",")
			}
			fmt.Fprintf(w, `{"id":%d,"body":"comment %d","path":"a.go","line":%d,"user":{"login":"dev"}}`, i, i, i)
		}
		fmt.Fprint(w, `]`)
	}))
	defer srv.Close()

	c := testGitHubClient(t, srv.URL)
	got, err := c.ListReviewComments(context.Background(), "o", "r", 1, 30)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 30 {
		t.Fatalf("got %d comments", len(got))
	}
	if got[0].Body != "comment 1" {
		t.Fatalf("first comment = %q, want the newest page's first body", got[0].Body)
	}
	if pages != 1 {
		t.Fatalf("pages = %d", pages)
	}
}

func TestListReviewCommentsReturnsShortList(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `[
			{"id":1,"body":"a","path":"a.go","line":1,"user":{"login":"dev"}},
			{"id":2,"body":" ","path":"a.go","line":2,"user":{"login":"dev"}},
			{"id":3,"body":"b","path":"a.go","line":3,"user":{"login":"dev"}}
		]`)
	}))
	defer srv.Close()

	c := testGitHubClient(t, srv.URL)
	got, err := c.ListReviewComments(context.Background(), "o", "r", 1, 30)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Fatalf("got %d comments, want non-empty bodies only", len(got))
	}
}

func testGitHubClient(t *testing.T, rawURL string) *clientImpl {
	t.Helper()
	base, err := url.Parse(rawURL + "/")
	if err != nil {
		t.Fatal(err)
	}
	gh := gogithub.NewClient(nil)
	gh.BaseURL = base
	return &clientImpl{client: gh}
}
