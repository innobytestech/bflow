package agents

import "regexp"

// Skill es una skill instalada en la herramienta del agente.
type Skill struct {
	Name        string
	Description string
	Path        string
}

// processTerms son términos del proceso que bflow ya maneja. Una skill que
// menciona varios en su descripción probablemente choca con el flujo (crear
// ramas, abrir PRs, mover tickets, aprobar specs a mano).
var processTerms = []struct {
	name string
	re   *regexp.Regexp
}{
	{"spec", regexp.MustCompile(`(?i)\bspecs?\b`)},
	{"feature", regexp.MustCompile(`(?i)\bfeatures?\b`)},
	{"hotfix", regexp.MustCompile(`(?i)\bhotfix`)},
	{"PR", regexp.MustCompile(`(?i)\bpull request|\bPRs?\b`)},
	{"walkthrough", regexp.MustCompile(`(?i)\bwalkthrough`)},
	{"discovery", regexp.MustCompile(`(?i)\bdiscovery\b`)},
	{"aprobar", regexp.MustCompile(`(?i)\baprob|\bapprov`)},
	{"qué sigue", regexp.MustCompile(`(?i)qué sigue|what'?s next`)},
	{"tracker", regexp.MustCompile(`(?i)\b(tracker|plane|jira|linear)\b`)},
	{"rama", regexp.MustCompile(`(?i)\b(ramas?|branch(es)?)\b`)},
	{"SDD", regexp.MustCompile(`(?i)\bsdd\b`)},
	{"ticket", regexp.MustCompile(`(?i)\b(tickets?|issues?)\b`)},
}

// ProcessTerms devuelve los términos de proceso que menciona la descripción
// si son dos o más: con uno solo suele ser otra cosa ("branch" de un árbol de
// decisiones). Es una heurística: sirve para avisar, no para bloquear.
func ProcessTerms(description string) []string {
	var found []string
	for _, t := range processTerms {
		if t.re.MatchString(description) {
			found = append(found, t.name)
		}
	}
	if len(found) < 2 {
		return nil
	}
	return found
}
