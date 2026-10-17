package review

import (
	"slices"
	"testing"

	"innobytes.tech/bflow/internal/vcs"
)

func added(text string) []vcs.AddedLine {
	return []vcs.AddedLine{{Path: "a.go", Line: 5, Text: text}}
}

func TestNarrativeCommentsRejectsTaskNarration(t *testing.T) {
	got, err := NarrativeComments(added("\t// T3: según R4"), "GH-9", DefaultCommentPatterns)
	if err != nil || !slices.Equal(got, []string{"a.go:5: // T3: según R4"}) {
		t.Errorf("got %v, %v", got, err)
	}
	got, _ = NarrativeComments(added("x := 1 // gh-151"), "GH-151", nil)
	if !slices.Equal(got, []string{"a.go:5: // gh-151"}) {
		t.Errorf("el ID debe buscarse siempre y sin mayúsculas: %v", got)
	}
	for _, c := range []string{"// ver la spec", "// la decisión fue X", "# walkthrough", "// seguimos según el plan"} {
		if got, _ := NarrativeComments(added(c), "GH-1", DefaultCommentPatterns); len(got) != 1 {
			t.Errorf("%q debía reportarse", c)
		}
	}
}

func TestNarrativeCommentsAllowsWhy(t *testing.T) {
	got, err := NarrativeComments(added("// el proveedor corta a 50 ids por llamada"), "GH-151", DefaultCommentPatterns)
	if err != nil || len(got) != 0 {
		t.Errorf("got %v, %v", got, err)
	}
	md := []vcs.AddedLine{{Path: "docs/x.md", Line: 1, Text: "<!-- T3 -->"}}
	if got, _ := NarrativeComments(md, "GH-1", DefaultCommentPatterns); len(got) != 0 {
		t.Errorf("los .md se saltan: %v", got)
	}
}

func TestNarrativeCommentsIgnoresCode(t *testing.T) {
	for _, c := range []string{`s := "// T3"`, `url := "http://x/T3"`, `*p = T3`, `r := R4 + T3`, `s := "a // T3"`, `#include "spec.h"`} {
		if got, _ := NarrativeComments(added(c), "GH-1", DefaultCommentPatterns); len(got) != 0 {
			t.Errorf("%q es código: %v", c, got)
		}
	}
}

func TestNarrativeCommentsCustomPatterns(t *testing.T) {
	if got, _ := NarrativeComments(added("// ver la spec"), "GH-1", []string{`TODO`}); len(got) != 0 {
		t.Errorf("los patrones propios reemplazan a los fijos: %v", got)
	}
	if got, _ := NarrativeComments(added("// TODO arreglar"), "GH-1", []string{`TODO`}); len(got) != 1 {
		t.Errorf("el patrón propio no aplicó")
	}
	if _, err := NarrativeComments(added("// x"), "GH-1", []string{`(`}); err == nil {
		t.Error("un patrón inválido es error")
	}
}

func TestIsCommentPrefixes(t *testing.T) {
	cases := []struct {
		in, want string
		ok       bool
	}{
		{"  // a", "// a", true},
		{"# a", "# a", true},
		{"/* a", "/* a", true},
		{" * a", "* a", true},
		{" */", "*/", true},
		{"*", "*", true},
		{"-- a", "-- a", true},
		{"<!-- a -->", "<!-- a -->", true},
		{"x := 1 // a", "// a", true},
		{"*p = 1", "", false},
		{"--x", "", false},
		{"url := \"http://x\"", "", false},
		{`s := "a // T3"`, "", false},
		{`c := 'a // b'`, "", false},
		{"r := `a // b`", "", false},
		{`s := "x\" // y"`, "", false},
		{`s := "a" // b`, "// b", true},
		{`s := "a\\" // b`, "// b", true},
		{`#include "spec.h"`, "", false},
		{`#define SPEC 1`, "", false},
		{`#pragma once`, "", false},
		{`#if T1`, "", false},
		{`#endif`, "", false},
		{`#include <x.h> // T3`, "// T3", true},
		{`# include x`, "# include x", true},
		{`#includes`, "#includes", true},
		{`# T1`, "# T1", true},
	}
	for _, c := range cases {
		got, ok := IsComment(c.in)
		if got != c.want || ok != c.ok {
			t.Errorf("IsComment(%q) = %q, %v; quiero %q, %v", c.in, got, ok, c.want, c.ok)
		}
	}
}
