package jobs

import (
	"strings"
	"testing"

	gh "github.com/Astraxx04/pr-reviewer/internal/github"
)

func TestCapDiffKeepsFirstFileAndNamesTheRest(t *testing.T) {
	big := strings.Repeat("line\n", 10)
	diff := []gh.FileDiff{
		{Filename: "a.go", Patch: big},
		{Filename: "b.go", Patch: big},
		{Filename: "c.go", Patch: "ok\n"},
	}
	kept, omitted := capDiff(diff, 12)
	if len(kept) != 2 || kept[0].Filename != "a.go" || kept[1].Filename != "c.go" {
		t.Fatalf("kept = %#v", kept)
	}
	if len(omitted) != 1 || omitted[0] != "b.go" {
		t.Fatalf("omitted = %#v", omitted)
	}
}

func TestCapDiffKeepsOversizedOnlyFile(t *testing.T) {
	diff := []gh.FileDiff{{Filename: "huge.json", Patch: strings.Repeat("x\n", 100)}}
	kept, omitted := capDiff(diff, 5)
	if len(kept) != 1 || len(omitted) != 0 {
		t.Fatalf("kept %d omitted %v", len(kept), omitted)
	}
}
