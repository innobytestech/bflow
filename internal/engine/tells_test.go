package engine

import (
	"strings"
	"testing"

	"innobytes.tech/bflow/internal/review"
)

func TestReviewIncompleteQuestionTells(t *testing.T) {
	m := review.Map{HasDocs: true, QuestionTells: []string{"Lo acepta (actual)"}}
	rej := reviewIncomplete(review.Coverage{}, m, true, "")
	if rej == nil || rej.Code != "review_incomplete" || !strings.Contains(rej.Reason, "- Lo acepta (actual)") ||
		!strings.Contains(rej.Reason, "Preguntas de producto") {
		t.Fatalf("rechazo: %+v", rej)
	}
	m.QuestionTells = nil
	if rej := reviewIncomplete(review.Coverage{}, m, true, ""); rej != nil {
		t.Errorf("sin marcas no debe rechazar: %+v", rej)
	}
}

func TestReviewIncompleteRedNoExits(t *testing.T) {
	m := review.Map{HasDocs: true, RedNoExits: []string{"`a.go`: cambia el estado"}}
	rej := reviewIncomplete(review.Coverage{}, m, true, "")
	if rej == nil || rej.Code != "review_incomplete" || !strings.Contains(rej.Reason, "- `a.go`: cambia el estado") ||
		!strings.Contains(rej.Reason, "caminos") {
		t.Fatalf("rechazo: %+v", rej)
	}
	m.RedNoExits = nil
	if rej := reviewIncomplete(review.Coverage{}, m, true, ""); rej != nil {
		t.Errorf("con caminos no debe rechazar: %+v", rej)
	}
}
