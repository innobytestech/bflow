package engine

import (
	"strings"
	"testing"
)

func TestHollowIn(t *testing.T) {
	goSrc := `package a

func TestVacia(t *testing.T) {}

func TestSalta(t *testing.T) {
	// pendiente
	t.Skip("contrato")
}

func TestSinCuerpo(t *testing.T) {
}

func TestSinBD(t *testing.T) {
	if os.Getenv("DB") == "" {
		t.Skip("sin BD")
	}
	assert(t, 1 == 1)
}

func TestReal(t *testing.T) {
	got := Suma(1, 2)
	if got != 3 { t.Fatal(got) }
}
`
	got := strings.Join(hollowIn("a_test.go", []byte(goSrc)), "\n")
	want := "a_test.go:3 TestVacia sin cuerpo\na_test.go:5 TestSalta se salta siempre\na_test.go:10 TestSinCuerpo sin cuerpo"
	if got != want {
		t.Errorf("go:\n%s\nwant:\n%s", got, want)
	}
	for name, src := range map[string]string{
		"a.spec.ts":  "it.skip('x', () => {})\ntest.todo('y')\nxit('z')\nit('real', () => {})",
		"test_a.py":  "@pytest.mark.skip\ndef test_a(): pass\n@pytest.mark.skipif(sys.platform == 'win32')\ndef test_b(): ...",
		"ATest.java": "@Disabled\nvoid a() {}\n@Test\nvoid b() {}",
		"ATests.cs":  "[Fact(Skip = \"luego\")]\npublic void A() {}\n[Fact]\npublic void B() {}",
	} {
		n := len(hollowIn(name, []byte(src)))
		want := map[string]int{"a.spec.ts": 3, "test_a.py": 1, "ATest.java": 1, "ATests.cs": 1}[name]
		if n != want {
			t.Errorf("%s: %d huecas, want %d", name, n, want)
		}
	}
}
