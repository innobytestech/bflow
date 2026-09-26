package markdown

import "testing"

func TestToHTML(t *testing.T) {
	cases := []struct{ name, in, want string }{
		{"párrafo", "hola mundo", "<p>hola mundo</p>"},
		{"escapa html", "a < b & c", "<p>a &lt; b &amp; c</p>"},
		{"negrita e itálica", "**Rechazado en spec:** falta *un* caso", "<p><strong>Rechazado en spec:</strong> falta <em>un</em> caso</p>"},
		{"código en línea", "usa `bflow check` antes", "<p>usa <code>bflow check</code> antes</p>"},
		{"enlace", "ver [PR #41](https://github.com/a/b/pull/41)", `<p>ver <a href="https://github.com/a/b/pull/41">PR #41</a></p>`},
		{"encabezados", "## Discovery\n\ntexto", "<h2>Discovery</h2><p>texto</p>"},
		{"lista", "- entra: X\n- no entra: Y", "<ul><li>entra: X</li><li>no entra: Y</li></ul>"},
		{"lista numerada", "1. uno\n2. dos", "<ol><li>uno</li><li>dos</li></ol>"},
		{"bloque de código", "```go\nif a < b {}\n```", "<pre><code>if a &lt; b {}</code></pre>"},
		{"saltos dentro de párrafo", "línea 1\nlínea 2", "<p>línea 1<br>línea 2</p>"},
		{"tabla", "| a | b |\n|---|---|\n| 1 | 2 |", "<table><thead><tr><th>a</th><th>b</th></tr></thead><tbody><tr><td>1</td><td>2</td></tr></tbody></table>"},
		{"emoji y acentos", "⏳ Esperando spec", "<p>⏳ Esperando spec</p>"},
		{"muchos códigos", "`a` `b` `c` `d` `e` `f` `g` `h` `i` `j` `k` `l`", "<p><code>a</code> <code>b</code> <code>c</code> <code>d</code> <code>e</code> <code>f</code> <code>g</code> <code>h</code> <code>i</code> <code>j</code> <code>k</code> <code>l</code></p>"},
		{"guion bajo en rutas no es itálica", "edita internal/foo_bar_test.go", "<p>edita internal/foo_bar_test.go</p>"},
	}
	for _, c := range cases {
		if got := ToHTML(c.in); got != c.want {
			t.Errorf("%s:\n got  %s\n want %s", c.name, got, c.want)
		}
	}
}

func TestToText(t *testing.T) {
	in := `<p><strong>Rechazado en spec:</strong> falta el caso &amp; más</p><ul><li>uno</li><li>dos</li></ul><p>a<br>b</p>`
	want := "Rechazado en spec: falta el caso & más\n- uno\n- dos\na\nb"
	if got := ToText(in); got != want {
		t.Errorf("got %q\nwant %q", got, want)
	}
}
