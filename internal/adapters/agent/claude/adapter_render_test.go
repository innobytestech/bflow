package claude

import "testing"

func TestOlder(t *testing.T) {
	cases := []struct {
		a, b string
		want bool
	}{
		{"2.1.270", "2.1.271", true},
		{"2.1.271", "2.1.271", false},
		{"2.1.283", "2.1.271", false},
		{"2.0.999", "2.1.0", true},
		{"3.0.0", "2.1.271", false},
		{"2.1.9", "2.1.10", true}, // numérico, no lexicográfico
	}
	for _, c := range cases {
		if got := older(c.a, c.b); got != c.want {
			t.Errorf("older(%s, %s) = %v", c.a, c.b, got)
		}
	}
}
