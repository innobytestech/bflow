# GH-20 · El reviewer aprueba sin leer el diff completo

## Brief

**Objetivo:** medir qué archivos del diff abre el reviewer, exigir que haya abierto los 🔴 antes de aprobar y hacer que las docs que describen el cambio se actualicen (o se justifique por qué no) antes de que el documenter termine.

**Entra**
- Registro al vuelo de las lecturas del reviewer (`Read`, `git diff`/`git show` con ruta, `cat`/`sed`/`head`/`tail`…) desde el hook que ya corre el guard, en Claude Code y OpenCode, en `.bflow/tasks/<ID>/reads.jsonl`.
- `bflow report --agent reviewer --verdict APPROVED` sale con código 2 si falta leer un 🔴, si una viñeta 🔴 no trae ruta o si el review-map no tiene `## Docs`. REJECTED nunca se bloquea por esto.
- Cobertura ("el reviewer leyó N de M archivos", no leídos de código, rutas 🔴 fuera del diff) en la gate walkthrough y en `bflow stats <ID>`.
- `bflow report --agent documenter --verdict DONE` sale con código 2 si una doc de `## Docs` no cambió en la rama ni está en `reports/docs.md` como "sin cambio: <motivo>".
- Oficio: el reviewer abre los 🔴 y escribe la ruta en cada uno; el walkthrough del documenter solo afirma las pruebas que constan en `check` o en `impl.md`.

**No entra**
- Una segunda pasada del reviewer después de documenting; umbral porcentual; piso de 🔴; cobertura de otros agentes (security-auditor incluido).

**Decisiones nuevas**
- [N] Hook nuevo de lectura: `Read` se suma al matcher del guard y el comando pasa a `bflow guard --reads`. Ese flag dice "esta instalación manda Read"; sin él la cobertura queda "no medida" y no bloquea. *Descartado:* un comando `bflow hook read` aparte (otro proceso por llamada) y medir al leer el transcript en `report` (el adaptador no sabe qué transcript es el del reviewer en curso).
- [N] Formato 🔴: cada viñeta empieza con una o más rutas en backticks (`` - `a.go`, `b.go`: … ``). bflow también exige que exista `## Docs` (puede decir `- ninguna`). *Descartado:* adivinar rutas en texto libre.
- [N] Ventana de lecturas: solo cuentan las registradas desde la última entrada a quality, así una ronda no hereda lecturas de la anterior.
- [N] Un `git diff` sin ruta cuenta como diff entero si `git diff --numstat base...HEAD` suma menos de 1,500 líneas (se mide al reportar). Un `git show` sin ruta no cuenta.
- [N] En OpenCode el plugin solo manda `read` de la subsesión `bflow-reviewer`. En Claude, el guard descarta el `Read` de otro agente antes de cargar la config.
- [N] `bflow doctor` avisa si `.claude/settings.json` no tiene `bflow guard --reads`.

**Riesgos**
- Latencia: en Claude cada `Read` de cualquier sesión arranca `bflow guard` (antivirus en Windows). El atajo sale antes de leer la config; si pesa, se mide con `stats`.
- La doc de `## Docs` que el implementer ya tocó cuenta como cambiada aunque siga incompleta: el reviewer la tiene a la vista en el diff.
- Rutas en Git Bash (`/c/...`), absolutas de Windows y relativas: se normalizan; lo que no se pueda resolver dentro del repo se ignora.
- El reviewer puede marcar pocos 🔴 para leer menos: lo frena que el humano ve la lista de no leídos.

**Tamaño:** M-L. Paquete nuevo `internal/review`, hooks de los dos adaptadores y el plugin, guard, engine (report y display), vcs (`DiffLines`), metrics y stats, oficios, docs.

## Discovery

En LOCAL-1 el reviewer aprobó en pocos segundos sin abrir archivos de riesgo, y el walkthrough y el README desactualizado los produjo el documenter, que corre después de quality. Decidido: medir lecturas desde las herramientas (no por autodeclaración), exigir solo los 🔴 en APPROVED, mostrar la cobertura en el walkthrough y en stats, y repartir las docs: el reviewer las lista y el documenter las cumple. Sin registro de lecturas no se bloquea.

## Requirements

**Registro de lecturas**
- **R1** [D] CUANDO el guard recibe una acción del subagente `bflow-reviewer` con `--reads` y la tarea activa está en quality, el sistema DEBE agregar a `.bflow/tasks/<ID>/reads.jsonl` una línea con ts, herramienta, rutas leídas (relativas al repo) y si fue diff entero, aunque la acción no lea nada.
- **R2** [D] El sistema DEBE contar como leído: un `Read` (cualquier rango) sobre el archivo; un `git diff`/`git show` sin `--stat`, `--numstat`, `--shortstat`, `--name-only`, `--name-status` ni `--dirstat` cuya ruta (después de `--`, existente en el repo o `rev:ruta`) sea el archivo o una carpeta que lo contiene; `cat`, `type`, `Get-Content`, `head`, `tail`, `sed`, `less`, `more`, `bat` o `nl` con el archivo como argumento.
- **R3** [D] CUANDO un `git diff` (no `git show`) no tiene ruta, el sistema DEBE registrarlo como diff entero.
- **R4** [D] El sistema DEBE normalizar las rutas (absolutas, relativas al cwd, `\` o `/`, `/c/...` de Git Bash, mayúsculas de la unidad en Windows) a rutas relativas al repo con `/`, y descartar las que caen fuera del repo.
- **R5** [N] CUANDO la acción es un `Read` de otro agente o de la sesión principal, o la fase no es quality, el guard NO DEBE escribir nada. Registrar nunca DEBE bloquear ni hacer fallar al guard.

**Exigencia al reviewer**
- **R6** [D] CUANDO el reviewer reporta en quality, el sistema DEBE calcular la cobertura contra `git diff --name-only base...HEAD` con las lecturas desde la última entrada a quality. Un diff entero cubre todos los archivos solo si el diff suma menos de 1,500 líneas.
- **R7** [D] CUANDO el reviewer reporta APPROVED y la cobertura está medida y algún archivo de una viñeta 🔴 que está en el diff no fue leído, el sistema DEBE rechazar con código 2 (`review_incomplete`) y listar las rutas que faltan.
- **R8** [N] CUANDO el reviewer reporta APPROVED y una viñeta 🔴 no empieza con una ruta en backticks, o el review-map no tiene la sección `## Docs`, el sistema DEBE rechazar con `review_incomplete` y decir qué falta, esté medida o no la cobertura.
- **R9** [D] CUANDO el reviewer reporta REJECTED, el sistema NO DEBE bloquearlo por cobertura ni formato.
- **R10** [D] CUANDO no hay en la ventana ninguna lectura registrada con `--reads`, la cobertura DEBE quedar "no medida" y R7 NO DEBE aplicar.
- **R11** [D] Una ruta 🔴 que no está en el diff DEBE ignorarse para R7 y aparecer como "fuera del diff" en la cobertura.
- **R12** [D] CUANDO bflow acepta un reporte del reviewer en quality, el sistema DEBE registrar en log.jsonl un evento `review_coverage` con la cobertura.

**Visibilidad**
- **R13** [D] CUANDO se abre la gate walkthrough y hay un `review_coverage`, el display DEBE empezar con "el reviewer leyó N de M archivos del diff", la lista de no leídos que no son pruebas ni docs (máximo 20 y "y K más") y las rutas 🔴 fuera del diff; sin medir, "cobertura del reviewer: no medida".
- **R14** [D] CUANDO se ejecuta `bflow stats <ID>` y la tarea tiene `review_coverage`, el sistema DEBE agregar una línea `revisión:` con la última cobertura y, con `--json`, `data.stats.review`. Sin ese evento, la salida DEBE quedar idéntica a la de hoy.

**Docs**
- **R15** [D] CUANDO el documenter reporta DONE en documenting, el sistema DEBE rechazar con código 2 (`docs_pending`) si una ruta de `## Docs` no está en el diff de la rama ni aparece en `reports/docs.md` como `` - `ruta`: sin cambio: <motivo> `` con motivo no vacío, y listar las que faltan. Sin review-map o sin rutas en `## Docs`, no aplica.
- **R16** [D] El oficio del reviewer DEBE pedir abrir cada 🔴 (o un `git diff` completo si el diff tiene menos de 1,500 líneas), la ruta en backticks al inicio de cada 🔴 y la sección `## Docs`; el del documenter, cumplir `## Docs` en la rama o en `reports/docs.md` y afirmar en el walkthrough solo las pruebas que constan en `check` o en `impl.md`. El contrato del documenter DEBE listar `reports/docs.md`.

**Instalación**
- **R17** [N] La configuración de Claude DEBE llamar `bflow guard --reads` con matcher `Bash|Edit|Write|MultiEdit|NotebookEdit|Read`; el plugin de OpenCode DEBE mandar `read` solo de la subsesión `bflow-reviewer` y llamar `guard --tool opencode --reads`.
- **R18** [N] CUANDO `.claude/settings.json` llama a `bflow guard` sin `--reads`, `bflow doctor` DEBE avisar "claude: el guard no ve Read; la cobertura del reviewer no se mide (corre bflow install claude)".

## Design

### Paquete nuevo `internal/review`

Lógica pura, sin I/O de git. Importa `guard` y `flow`.

```go
const (
	ReadsFile    = "reads.jsonl"
	WholeDiffMax = 1500 // líneas: con menos, un git diff sin ruta cubre todo
)

// Read es una herramienta que usó el reviewer, tal como se guarda.
type Read struct {
	TS       time.Time `json:"ts"`
	Tool     string    `json:"tool"`            // guard.Read | guard.Bash | guard.Edit | guard.Write
	Paths    []string  `json:"paths,omitempty"` // relativas al repo, con /; pueden ser carpetas (pathspec de git)
	Whole    bool      `json:"whole,omitempty"` // git diff sin ruta
	ReadHook bool      `json:"read_hook"`       // vino de guard --reads
}

func Normalize(root, cwd, p string) (string, bool)            // R4
func FromAction(a guard.Action, root, cwd string, readHook bool, now time.Time) Read // R2, R3
func ParseReads(r io.Reader) []Read                           // salta líneas inválidas
func IsDoc(path string) bool                                  // .md .mdx .rst .txt .adoc o bajo docs/

// Map es lo que bflow lee del review-map.
type Map struct {
	Red       []string // rutas de las viñetas 🔴, normalizadas (sin :línea ni #L), sin duplicados
	RedNoPath []string // viñetas 🔴 sin ruta inicial, recortadas a 80 runas
	Docs      []string // rutas de ## Docs
	HasDocs   bool
}
func ParseMap(md string) Map

// Coverage es la cobertura de una corrida del reviewer.
type Coverage struct {
	Measured    bool     `json:"measured"`
	Total       int      `json:"total"` // archivos del diff
	Read        int      `json:"read"`
	Whole       bool     `json:"whole,omitempty"`  // un diff entero cubrió todo
	Unread      []string `json:"unread,omitempty"` // no leídos que no son pruebas ni docs
	UnreadOther int      `json:"unread_other,omitempty"`
	RedMissing  []string `json:"red_missing,omitempty"`
	RedOutside  []string `json:"red_outside,omitempty"`
}
func Measure(diff []string, diffLines int, reads []Read, red []string, isTest func(string) bool) Coverage
func (c Coverage) Lines() []string // texto para display y stats, sin emojis
func DocsPending(listed, diff []string, docsReport string) []string // R15
```

- `ParseMap`: la sección 🔴 es el encabezado `#…` que contiene "🔴" hasta el siguiente encabezado de igual o mayor nivel; la de docs, el encabezado cuyo texto es "Docs" (sin distinguir mayúsculas). En ellas, una viñeta es una línea `- `, `* ` o `N. ` sin sangría; sus rutas son los code spans seguidos al inicio, separados por `, ` o espacio, antes de `:` o de texto.
- `Measure`: un archivo f del diff está leído si alguna `Paths` es f o una carpeta que lo contiene (`f` empieza con `p + "/"`), o si hay un `Whole` y `diffLines < WholeDiffMax`. `Measured` = existe al menos un `Read` con `ReadHook`. Sin medir, `Read` = 0 y `RedMissing` vacío. `Unread` excluye `isTest(f) || IsDoc(f)`, que suman a `UnreadOther`.
- `FromAction` en Bash: separa con `guard.Segments` (nueva, exportada, la que usan `TaskScoped` y `bash`), quita comillas, salta `git -C x`, `-c k=v` y `--no-pager`. En `cat…nl` toma los argumentos que no empiezan con `-` y existen como archivo. En git, sin rutas y en `diff` → `Whole`.

### Guard y adaptadores

- `guard.Read = "read"`; `Evaluate` lo permite (cae en el `default`).
- claude `ParsePreToolUse`: `case "Read"` → `guard.Action{Tool: guard.Read, Path: file_path, …}`.
- opencode `ParseActions`: `case "read"` → `mk(guard.Read, args.FilePath)`.
- `plugin.js`: `GUARDED` suma `"read"`; si `input.tool === "read"` y la sesión no es subsesión con agente `bflow-reviewer`, sale sin lanzar proceso; el comando pasa a `["guard","--tool","opencode","--reads"]`.
- `adapters/claude/settings.json`: matcher `Bash|Edit|Write|MultiEdit|NotebookEdit|Read` y `bflow guard --reads`. `mergeSettings` ya reemplaza las entradas de bflow.

### Hook (`cli/hookcmds.go`)

- `guard` gana `fs.Bool("reads", …)`.
- Atajo: si todas las acciones son `guard.Read` y ninguna es del reviewer (`strings.CutPrefix(a.Agent, flow.SubagentPrefix)` ≠ `reviewer`), devuelve `allowed` antes de `config.Load`.
- Después de evaluar sin rechazo: `recordReads(c, cfg, acts, cwd, reads bool)`. Para acciones del reviewer, carga la tarea activa; si está en quality, escribe con `e.Store.AppendFile(id, review.ReadsFile, …)` una línea por acción (R1). Ignora cualquier error (R5).

### Engine

- `vcs.Git` gana `DiffLines(ctx, base string) (int, error)`: suma agregadas y borradas de `git diff --numstat base...HEAD`; los binarios (`-`) cuentan 0. Lo implementan el adaptador git y el `fakeGit` de pruebas.
- `e.diffBase()` saca el cálculo `remote/base` que hoy está en `taskTests`.
- `e.reviewCoverage(ctx, id) (review.Coverage, review.Map, error)`: lee el review-map, `reads.jsonl` desde la última entrada de log con `To == quality` y `From != quality`, `DiffNames` y `DiffLines`. Si git falla o no hay base, `Measured=false`.
- En `apply`, dentro de `Update`, con `ev.Kind == EvReport` y `rec.Flow.Phase == Quality` y agente `reviewer` (sin prefijo):
  - con APPROVED arma un `review_incomplete` si hay `RedMissing` (medida), `RedNoPath` o `!HasDocs`, con un bloque por problema y "reporta APPROVED otra vez";
  - si el reporte se acepta, agrega la entrada `review_coverage` (Data = la `Coverage` como map, más `round`).
- Con `EvReport`, `DoneV` y documenting, después de la comprobación de `uncommitted`: `review.DocsPending(m.Docs, DiffNames, reports/docs.md)` → `docs_pending` con la lista y el formato esperado.
- `withDisplay`: en `GateWalkthrough`, antes de lo que muestra `Show`, agrega `**Cobertura del reviewer:**` + `Lines()` del último `review_coverage` (no aparece si no hay).

### Métricas y stats

- `metrics.TaskStats` gana `Review *review.Coverage \`json:"review,omitempty"\``, tomado del último `review_coverage` de la tarea (`Compute`).
- `renderStats` agrega `  revisión: <primera línea de Lines()>` si `st.Review != nil`.

### Agentes y doctor

- `catalog["documenter"].writes` = `walkthrough.md`, `reports/docs.md`.
- `craft/reviewer.md` y `craft/documenter.md` según R16; `bflow render` regenera `.claude/agents/bflow-*.md`.
- doctor (claude): si el archivo tiene `bflow guard` pero no `bflow guard --reads`, el aviso de R18.

### Descartado

- Autodeclaración del reviewer ("leí X"): no se puede verificar.
- Exigir lecturas en 🟡 o por porcentaje: encarece los diffs grandes sin apuntar al riesgo.
- Releer el review-map al cambiar de ronda: la ventana por la entrada a quality ya separa las rondas.

### Seguridad

- Authz: la exigencia vive en bflow, no en el agente. Un subagente no puede escribir `reads.jsonl` (está en `.bflow/`, fuera de sus `writes`). Igual podría correr `cat` sin leer de verdad, y eso no se puede distinguir.
- Validación: el review-map, `reports/docs.md` y `reads.jsonl` se parsean como texto; las líneas inválidas se ignoran; `Store.Path` impide salir de la carpeta de la tarea.
- Datos sensibles: `reads.jsonl` guarda rutas relativas al repo, sin comandos ni contenido; lo que cae fuera del repo (home, temporales) se descarta antes de escribir.
- Errores: el registro nunca falla ni bloquea al guard; los rechazos dicen qué falta y cómo corregirlo, sin rutas absolutas.

## Tasks

- [ ] **T1 Contrato** (R1-R18): tipos y firmas sin lógica (`internal/review` completo, `guard.Read`, `guard.Segments`, `vcs.Git.DiffLines`, `TaskStats.Review`, flag `--reads`) y estas pruebas, que deben fallar:
  - review: `TestNormalize`, `TestFromActionRead`, `TestFromActionBash`, `TestFromActionGitWhole`, `TestParseReadsSkipsBadLines`, `TestParseMap`, `TestMeasure`, `TestMeasureWholeDiffLimit`, `TestMeasureUnmeasured`, `TestDocsPending`
  - adaptadores: `TestParsePreToolUseRead` (claude), `TestParseActionsRead` (opencode), `TestPluginReadsOnlyReviewer`, `TestSettingsGuardSeesRead`
  - git: `TestDiffLines`
  - cli: `TestGuardRecordsReviewerReads`, `TestGuardIgnoresOtherReads`, `TestGuardRecordNeverBlocks`, `TestStatsReviewCoverage`, `TestDoctorWarnsGuardWithoutReads`
  - engine: `TestReviewerApprovedNeedsRedRead`, `TestReviewerApprovedWholeSmallDiff`, `TestReviewerRejectedNotBlocked`, `TestReviewerUnmeasuredNotBlocked`, `TestReviewerNeedsRedPathsAndDocs`, `TestReviewCoverageLogged`, `TestWalkthroughShowsCoverage`, `TestDocumenterDocsPending`
  - metrics: `TestComputeReviewCoverage`; agents: `TestDocumenterWritesDocsReport`
- [ ] **T2 review** (R2-R4, R6, R10, R11, R15): `Normalize`, `FromAction`, `ParseReads`, `IsDoc`, `ParseMap`, `Measure`, `Lines`, `DocsPending` y `guard.Segments`. Pasan las pruebas de review y las de guard que ya existen.
- [ ] **T3 Captura** (R1, R5, R17): `guard.Read`, adaptadores claude y opencode, `plugin.js`, `settings.json`, flag `--reads`, atajo y `recordReads`. Pasan las pruebas de adaptadores y `TestGuard*`.
- [ ] **T4 Exigencia** (R6-R12, R15): `DiffLines` (git y fake), `diffBase`, `reviewCoverage`, chequeos en `apply` para reviewer y documenter, y el evento `review_coverage`. Pasan `TestDiffLines` y las de engine menos `TestWalkthroughShowsCoverage`.
- [ ] **T5 Visibilidad** (R13, R14): `withDisplay`, `TaskStats.Review`, `renderStats`. Pasan `TestWalkthroughShowsCoverage`, `TestComputeReviewCoverage` y `TestStatsReviewCoverage`, y las de stats que ya existen.
- [ ] **T6 Oficios y doctor** (R16, R18): `craft/reviewer.md`, `craft/documenter.md`, `writes` del documenter, aviso de doctor, `bflow render` y `bflow install claude` en este repo (`.claude/agents/*`, `.claude/settings.json`). Pasan `TestDocumenterWritesDocsReport` y `TestDoctorWarnsGuardWithoutReads`.
- [ ] **T7 Docs** (R13-R17): `docs/guia.md` (gates: la cobertura del walkthrough; Medir: la línea `revisión:`), README (Qué garantiza, hooks), `adapters/claude/README.md` y `adapters/opencode/README.md` (Read y `--reads`), `CHANGELOG.md`.
