package claude

import (
	"strings"
	"testing"
)

func TestMergeSettingsKeepsShellOperators(t *testing.T) {
	cur := []byte(`{"hooks":{"Stop":[{"hooks":[{"type":"command","command":"cd x && npm test 2>&1 <in"}]}]},"a<b":"é"}`)
	out, _, _, err := mergeSettings(cur, []byte(`{"permissions":{"allow":["Bash(bflow *)"]}}`))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{`"cd x && npm test 2>&1 <in"`, `"a<b": "é"`} {
		if !strings.Contains(string(out), want) {
			t.Errorf("falta %s sin escapar:\n%s", want, out)
		}
	}
	if strings.Contains(string(out), `\u00`) {
		t.Errorf("escapó caracteres:\n%s", out)
	}
}
