# GH-151 · [02] Comentarios en el código: el implementer narra la tarea y sesga la revisión

## Brief

**Objetivo:** que los comentarios que agrega un agente expliquen el por qué del código y no narren la tarea, y que bflow rechace el DONE cuando una línea de comentario agregada en el diff menciona la tarea, la spec o sus criterios.

**Entra**
- Oficio del implementer (`internal/agents/craft/implementer.md`): un comentario explica lo que el código no deja ver (invariante, trampa, restricción externa). Nunca narra la tarea, la spec, la decisión tomada ni lo que "se validó". Eso va en `reports/impl.md` y en el PR.
- Oficio del reviewer (`internal/agents/craft/reviewer.md`): un comentario no prueba que el código haga lo que dice.
- Rechazo `narrative_comment` en `report DONE` del implementer y del documenter, y el mismo fallo en `bflow check --verify` (sale `not_verified`, igual que las pruebas congeladas).
- Config: `flow.comment_patterns` (reemplaza los patrones fijos) y `flow.narrative_comments: false` (apaga la regla).
- Docs: `docs/guia.md`, `docs/configuracion.md` y `CHANGELOG.md`.
- Arreglo de `TestConcurrentCreateUniqueIDs` (`internal/adapters/tracker/local`), que falla en Windows bajo la carga de `bflow check`: 12 de 30 `Create` salen con `la tarea está bloqueada por otro proceso de bflow` a los 10 s. Sin esto el check no queda en verde.

**No entra**
- Walkthrough y gate del PR: el diff ya pasó por el DONE.
- Comentarios que ya estaban en el código: solo cuentan las líneas agregadas (`+`) del diff `base...HEAD`.
- Detectar comentarios con un parser por lenguaje. Se usan prefijos de texto.
- Docs, `specs/` y archivos `.md`: se escanean solo las rutas de `check.code_paths`, sin los `.md`.

**Decisiones nuevas**
- [N] Se detecta por prefijo: una línea agregada que, sin los espacios iniciales, empieza con `//`, `#`, `/*`, `*` (seguido de espacio, `/` o fin de línea), `-- ` o `<!--`, o que lleva un `//` final con espacio antes (`x := 1 // T3`). Descartada: un parser por lenguaje. Pide una dependencia por lenguaje y el repo destino puede traer cualquiera.
- [N] Se apaga con un booleano aparte, `flow.narrative_comments: false`, igual que `flow.scout`. `comment_patterns` reemplaza los patrones fijos. El ID de la tarea se busca siempre. Descartada: apagarla con `comment_patterns: []`. Una lista vacía y una clave ausente se confunden al combinar las capas de config.
- [N] `check --verify` falla con `not_verified` y el detalle `comentarios narrativos: …`. Descartada: un código nuevo. El reviewer y el CI ya leen `not_verified` como "el check no vale".
- [N] También aplica al DONE del documenter, que agrega comentarios de documentación. Descartada: aplicarla solo al implementer. El documenter podría narrar sin que nada lo detenga.
- [N] El unlock de `store.LockFile` reintenta `os.Remove`, como ya lo hace `WriteAtomic` con el rename, y el tracker local serializa en memoria los `Create` y `modify` del mismo proceso sobre la misma carpeta. Descartada: subir el timeout de 10 s. Si el lock se queda pegado, solo retrasa el fallo.

**Riesgos**
- Falsos positivos: `# T1` en un YAML dentro de `code_paths`, o un `spec` legítimo ("spec de OpenAPI"). Se resuelven con `comment_patterns` o apagando la regla, y el rechazo dice cómo.
- En este repo, una prueba con un fixture multilínea cuya línea empieza con `// T3` hace que se rechace su propio DONE. Los fixtures se arman con `"\n"`, no con raw strings multilínea.
- La causa del lock no se reproduce aislada (pasa con `-race -count=15`). La hipótesis es que en Windows el antivirus abre el `.seq.lock` recién creado, el `os.Remove` del unlock falla en silencio y el lock queda vivo hasta que se vuelve viejo (2 min).
- `\b` de Go solo reconoce ASCII: los patrones de `según` y `decisión` no llevan `\b` al final.

**Tamaño:** M. Un método nuevo en `vcs.Git`, un escáner puro en `internal/review`, dos llamadas (engine y CLI), config, dos oficios, docs y pruebas, más el arreglo del lock (`internal/store`, `internal/adapters/tracker/local`).

## Discovery

<!-- Discovery completo; lo escribe bflow al cerrar el gate discovery. No lo edites. -->

## Requirements

<!-- Carril light: los criterios van dentro de Tasks. -->

## Design

<!-- Carril light: los nombres y firmas van dentro de Tasks. -->

## Tasks

- [x] T1 Leer las líneas agregadas del diff.
  - Se agrega a `vcs.Git` (`internal/vcs/vcs.go`) `DiffAdded(ctx context.Context, base string, paths []string) ([]AddedLine, error)`, con `type AddedLine struct { Path string; Line int; Text string }`. `Line` es el número de línea en el archivo nuevo.
  - Se implementa en `internal/adapters/vcs/git/git.go` con `git diff --unified=0 --no-color --no-ext-diff --no-renames <base>...HEAD -- <paths>`. La ruta sale de `+++ b/<ruta>` y la línea se cuenta desde `@@ -a,b +c,d @@`. Se saltan `+++`, `/dev/null` y los binarios.
  - CUANDO el diff agrega `// T3: según R4` en la línea 5 de `a.go`, el sistema DEBE devolver `{a.go, 5, "// T3: según R4"}`. Las rutas usan `/`.
  - `fakeGit` (`internal/engine/vcs_test.go`) gana un campo `added []vcs.AddedLine`.
  - Prueba: `TestDiffAdded` en `internal/adapters/vcs/git`, con un repo real (dos commits, hunks en dos archivos y una línea borrada que no cuenta).
- [x] T2 Escáner puro en `internal/review/comments.go`.
  - `var DefaultCommentPatterns = []string{`\bR\d+\b`, `\bT\d+\b`, `(?i)\bspec\b`, `(?i)\bsegún`, `(?i)\bdecisi[oó]n`, `(?i)\bwalkthrough\b`}`.
  - `func IsComment(text string) (comment string, ok bool)` aplica los prefijos de la decisión [N] y devuelve solo el texto del comentario. Con un `//` final, devuelve desde el `//`.
  - `func NarrativeComments(lines []vcs.AddedLine, taskID string, patterns []string) ([]string, error)` devuelve `ruta:línea: texto` por cada línea que es comentario y cumple algún patrón o contiene el ID de la tarea (sin distinguir mayúsculas). Un patrón inválido es error. Se saltan las rutas que terminan en `.md`.
  - CUANDO la línea es `// T3: según R4`, el sistema DEBE reportarla. CUANDO es `// el proveedor corta a 50 ids por llamada`, NO DEBE. CUANDO es código (`s := "// T3"`, `url := "http://x/T3"`, `*p = T3`), NO DEBE. CUANDO es `x := 1 // GH-151`, DEBE.
  - Pruebas: `TestNarrativeCommentsRejectsTaskNarration`, `TestNarrativeCommentsAllowsWhy`, `TestNarrativeCommentsIgnoresCode`, `TestNarrativeCommentsCustomPatterns` y `TestIsCommentPrefixes` (tabla con `//`, `#`, `/*`, `* `, `-- ` y `<!--`).
- [x] T3 Config.
  - En `config.Flow` (`internal/config/config.go`) se agregan `NarrativeComments *bool` (`yaml:"narrative_comments,omitempty"`) y `CommentPatterns []string` (`yaml:"comment_patterns,omitempty"`), cada uno con su comentario de documentación.
  - `Config.Validate` DEBE rechazar un patrón que no compile con `regexp.Compile`: `flow.comment_patterns: %q no es una expresión regular válida`.
  - Pasan a `flow.Config` como `NarrativeComments bool` (true salvo un `false` explícito) y `CommentPatterns []string` (los fijos si la lista viene vacía), igual que `PublicPaths`.
  - Pruebas: `TestValidateCommentPatterns` y `TestFlowCommentDefaults` en `internal/config`.
- [x] T4 Rechazo en DONE y en `check --verify`.
  - `func (e *Engine) NarrativeComments(ctx context.Context, id string) []string`, en `internal/engine/review.go`, devuelve nil si la regla está apagada, si no hay git o si no hay base. Si no, llama a `Git.DiffAdded(ctx, e.diffBase(), e.Cfg.Check.CodePaths)` y a `review.NarrativeComments`. Si git falla, devuelve nil, igual que `changelogRequired`.
  - En `engine.go`, en los bloques DONE de `Implementing` (después de `bflowInDiff` y antes de `ev.CheckOK = true`) y de `Documenting` (después de `bflowInDiff`): CUANDO hay hallazgos, el sistema DEBE devolver `flow.Rejection{Code: "narrative_comment"}`. El Reason lista hasta 20 `ruta:línea: texto` (con "y N más" si hay más) y dice: "Un comentario explica el por qué que el código no deja ver; lo de la tarea, la spec o lo validado va en reports/impl.md. Quítalo o reescríbelo, commitea y reporta DONE otra vez. Si el patrón no aplica a este repo, ajusta flow.comment_patterns o apaga flow.narrative_comments."
  - En `internal/cli/checkcmds.go`, con `--verify`: CUANDO `ok` es true, hay `id` y hay hallazgos, el sistema DEBE dejar `ok=false` con el detalle `comentarios narrativos: <ruta:línea>, …`. La salida es `not_verified` con código de error.
  - Pruebas: `TestDoneRejectsNarrativeComment` y `TestDoneAllowsWhyComment` (implementer), `TestDocumenterDoneRejectsNarrativeComment` y `TestDoneNarrativeCommentsDisabled` en `internal/engine`. `TestVerifyFailsOnNarrativeComment`, e2e en `cmd/bflow`, con un commit real que agrega `// T3: según R4`.
- [x] T5 Oficios, docs y changelog.
  - `implementer.md` DEBE llevar esta viñeta: "Un comentario explica el por qué que el código no deja ver (invariante, trampa, restricción externa). Nunca narra la tarea, la spec, la decisión tomada ni lo que se validó; eso va en `reports/impl.md` y en el PR. bflow rechaza el DONE con `narrative_comment` si un comentario agregado cita el ID, R<n>, T<n>, la spec o el walkthrough."
  - `reviewer.md`, junto a "Sin evidencia en el código…", DEBE decir: "Un comentario no es evidencia: verifica que el código haga lo que dice."
  - Se corre `bflow render` para regenerar `.claude/agents/` y `.opencode/agents/`. `internal/agents/agents_test.go` pasa.
  - `docs/guia.md`: en "Ajustar a los agentes", qué comentarios se esperan y por qué; en "Cuando algo se atora", qué hacer con `narrative_comment`. `docs/configuracion.md`: `flow.comment_patterns` y `flow.narrative_comments`, con un ejemplo. Una entrada en `CHANGELOG.md`.
  - `bflow check` pasa.
- [x] T6 Arreglo de `TestConcurrentCreateUniqueIDs`.
  - En `internal/store/store.go`, el unlock que devuelve `LockFile` DEBE reintentar `os.Remove(path)` mientras el error no sea `os.ErrNotExist`, hasta 50 veces con espera de `2+i` ms (lo mismo que `WriteAtomic`). Se agrega `var removeFile = os.Remove` en el paquete para que la prueba inyecte fallos. La firma de `LockFile` no cambia.
  - CUANDO `os.Remove` falla las primeras 3 veces y la cuarta pasa, el sistema DEBE dejar el lock borrado, y un `LockFile` posterior sobre la misma ruta DEBE obtenerlo sin esperar.
  - En `internal/adapters/tracker/local/local.go`, `Create` y `modify` toman primero un `*sync.Mutex` del proceso, guardado en `var dirLocks sync.Map` con clave `filepath.Clean(t.Dir)` (para `Create`) o `filepath.Clean(t.Dir)+"/"+id` (para `modify`), y después el lock de archivo de siempre, que sigue cubriendo a otros procesos.
  - CUANDO 30 goroutines llaman a `Create` sobre la misma carpeta con instancias distintas de `Tracker`, el sistema DEBE devolver 30 IDs distintos y ningún `ErrLocked`.
  - Pruebas: `TestLockFileUnlockRetriesRemove` en `internal/store`. `TestConcurrentCreateUniqueIDs` no cambia y DEBE pasar dentro de `bflow check` (`go test -count=1 -race ./...`).
  - `bflow check` pasa en verde.
