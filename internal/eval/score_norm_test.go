package eval

import (
	"slices"
	"testing"
)

func TestFindingsMarkdownDecoratedRefs(t *testing.T) {
	note := "**internal/a.go:10**\n[internal/a.go:20]\n> internal/a.go:30\n_internal/a.go:40_\n* internal/a.go:50\n"
	got := keys(Findings(note, nil, "/wt"))
	want := []string{"internal/a.go:10-10", "internal/a.go:20-20", "internal/a.go:30-30", "internal/a.go:40-40", "internal/a.go:50-50"}
	if !slices.Equal(got, want) {
		t.Fatalf("got %v want %v", got, want)
	}
}

func TestScoreNormalizesDefectFile(t *testing.T) {
	for _, file := range []string{"./internal/a.go", `internal\a.go`, `.\internal\a.go`} {
		s := ScoreCase([]Defect{{File: file, Lines: "100-110", What: "a"}}, []Finding{{"internal/a.go", 105, 105}})
		if len(s.Caught) != 1 || len(s.FalsePositives) != 0 {
			t.Fatalf("%s: %+v", file, s)
		}
	}
}
