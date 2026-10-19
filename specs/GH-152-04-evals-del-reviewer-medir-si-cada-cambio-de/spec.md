# GH-152 · [04] Evals del reviewer: medir si cada cambio de oficio atrapa más defectos

## Brief

**Objetivo:** `bflow eval reviewer` corre el reviewer sobre casos con defectos conocidos y deja en `.bflow/evals/<fecha>.json` cuántos atrapó, cuántos no y cuántos posibles falsos positivos marcó, con tokens y tiempo, para comparar oficios (el actual contra el de `v0.1.0-rc.5`).

**Entra:** formato de caso `.bflow/evals/reviewer/<caso>/case.yaml`; validación; corrida de un caso a la vez con `next: spawn`; `bflow eval reviewer report`; puntaje determinista (±5 líneas); tokens y tiempo por caso; `--craft <ref>`, `--case`, `--abort`, `--from-fixes`; agente `bflow-eval-reviewer`; excepciones del guard y del `.gitignore`; 5 o más casos curados del repo; docs y changelog.

**No entra:** comparar dos resultados con un comando (se leen los totales de cada JSON); juez LLM; correr casos en paralelo; evals de otros agentes; lanzar el agente desde bflow (headless).

**Decisiones nuevas**
- [N] El agente `bflow-eval-reviewer` es fijo (lo genera `bflow render` solo si existe `.bflow/evals/reviewer/`) y lee el oficio del ref desde un archivo que recibe en `craft`. Descartado: regenerar el agente por corrida con el oficio en el cuerpo; Claude Code puede no ver un agente creado a media sesión y `render --check` lo vería como sobrante.
- [N] Tokens: suma de las llamadas del agente `eval-reviewer` en los `calls.jsonl` (de tareas y sin tarea) dentro de la ventana spawn-report, calculada en la siguiente invocación (el hook de tokens corre al terminar el subagente). Descartado: crear una tarea sintética por caso en `.bflow/tasks/`, que ensucia status, panel y métricas.
- [N] Hallazgo = cada referencia `ruta:N` o `ruta:N-M` del `--note` y de los `.md` que el agente deja en `out`, salvo las líneas de `caminos:`, `cubre:`, `el código responde` y las secciones `## Docs` y `## Pruebas sin ejecutar`. Descartado: leer solo `## Seguimiento`, que no existía en rc.5 (rc.5 ponía los menores en `security.md`).
- [N] El report del eval no aplica las reglas de formato del review-map del flujo (🔴 con ruta, `caminos:`, cobertura de lecturas): solo exige que exista el review-map. Descartado: aplicarlas, porque castigan al oficio de rc.5 por reglas que no conocía.
- [N] Casos `draft: true` (los esqueletos de `--from-fixes`) se omiten y se listan; el resto mal formado bloquea la corrida. Descartado: rechazar todo mientras haya un esqueleto sin llenar.
- [N] Un caso a la vez; repetir `bflow eval reviewer` retoma el caso en curso. Descartado: paralelo, que mezcla la ventana de tokens.
- [N] Versionar `.bflow/evals/` exige `!evals/` y `!evals/**` en `.bflow/.gitignore` (bflow los agrega) y que el `.gitignore` raíz no ignore `.bflow/` entero (bflow avisa; en este repo se cambia a `.bflow/*` + `!.bflow/evals/`). El guard permite `git add` de `.bflow/evals/`.

**Riesgos**
- El agente lee el oficio de un archivo, no de su prompt de sistema: ambos oficios se comparan igual, pero la cifra absoluta puede diferir del reviewer real.
- Si la sesión principal tiene marca de una tarea, los tokens del eval caen en esa tarea (se cuentan igual por nombre de agente, pero ensucian su `stats`).
- Curar 5 casos con archivo:línea exactos es trabajo de lectura de historia (GH-159, #108 y otros escapes).
- `git worktree` dentro de `.bflow/cache/` en Windows: rutas largas; la limpieza usa `--force` y `prune`.

**Tamaño:** grande (paquete nuevo `internal/eval`, comando, agente, guard, dataset). 7 tareas.

## Discovery

# Discovery GH-152: evals del reviewer

## Alcance
- Comando nuevo `bflow eval reviewer` (patrón Register(&Command{...}) en internal/cli). Corre el reviewer sobre los casos de `.bflow/evals/reviewer/<caso>/` y reporta atrapados / no atrapados / posibles falsos positivos, con tokens y tiempo por caso y en total.
- Resultado en `.bflow/evals/<fecha>.json` para comparar entre versiones del oficio.
- Criterio de terminado: al menos 5 casos del propio repo; correr el eval con el oficio actual y con el de v0.1.0-rc.5 y poder decir cuál atrapa más.

## Ejecución (decidido)
- bflow NO lanza agentes por su cuenta. `bflow eval reviewer` prepara cada caso (worktree o rama temporal con el diff base..head del caso) y responde `next: spawn` de bflow-reviewer por caso, con sus args y su comando report, igual que el flujo. El report de cada caso deja el review-map y bflow lo califica.
- Se descartó que bflow invoque claude -p / opencode run en modo headless: se acoplaría a un cliente y el gasto correría sin control.
- Tokens y tiempo: reutilizar los eventos `tokens` existentes (internal/metrics/models.go) atribuidos a la corrida del eval; tiempo = desde el spawn hasta el report del caso. Se registra el modelo usado.

## Dataset (decidido)
- Hoy `.bflow/log.jsonl` no tiene eventos con `fixes`, así que `--from-fixes` saldría vacío.
- Casos iniciales (5 o más) curados a mano desde escapes conocidos del repo (p. ej. GH-159, #108): commit base y head más la lista de defectos esperados (archivo + rango de líneas + descripción breve).
- `bflow eval reviewer --from-fixes` genera esqueletos de caso desde los eventos `fixes` del log cuando existan (hotfix ligado con `start --fixes`: diff de la tarea original que el reviewer aprobó). El humano completa los defectos esperados.
- Se descartó minar commits fix(GH-N) con blame: da casos ruidosos que habría que revisar igual.

## Puntaje (decidido)
- Determinista: un defecto esperado cuenta como atrapado si el review-map tiene un hallazgo en el mismo archivo dentro del rango esperado, con tolerancia de ±5 líneas.
- Los hallazgos que no coinciden con ningún defecto esperado se reportan como posibles falsos positivos, sin etiquetarlos.
- Se descartó un juez LLM porque no es determinista y gasta tokens.

## Versión del oficio (decidido)
- `--craft <ref git>` (p. ej. v0.1.0-rc.5) renderiza el oficio del reviewer de ese ref como agente del eval. Sin ref, se usa el oficio del árbol. Verificar las rutas del oficio en rc.5 (`.claude/agents/bflow-reviewer.md` no existía en ese tag; internal/agents/agents.go sí).
- El resultado guarda el ref, el hash del oficio y el modelo.

## Versionado
- Los casos (`.bflow/evals/reviewer/`) y los resultados (`.bflow/evals/<fecha>.json`) se versionan en git.

## Errores y restricciones
- Caso mal formado (sin base/head o sin defectos esperados) o ref inexistente: rechazo con código 2 y `data.reason`; no se corre nada.
- Salida para agentes en JSON compacto (economía de tokens). La UI para personas usa texto en color, sin emojis.
- Hay que limpiar los worktrees temporales al terminar o al fallar.

## Requirements

**Casos y arranque**

- R1 [D] CUANDO se corre `bflow eval reviewer` sin corrida abierta y algún caso seleccionado sin `draft: true` está mal formado (falta `base` o `head`, alguno no resuelve a un commit, no tiene defectos, un defecto tiene ruta vacía, absoluta o con `..`, o un rango que no es `N` ni `N-M` con 1 ≤ N ≤ M), el sistema DEBE rechazar con código 2, `code: case_invalid` y `data.reason` con una línea `<caso>: <problema>` por cada problema, sin crear corrida ni worktrees.
- R2 [D] CUANDO `--craft <ref>` no resuelve a un commit, el sistema DEBE rechazar con código 2 y `code: craft_ref_unknown`; CUANDO el ref no contiene `internal/agents/craft/reviewer.md`, con `code: craft_missing`. En ambos casos sin crear nada. Un ref que empieza con `-` cuenta como inexistente.
- R3 [N] CUANDO un caso tiene `draft: true`, el sistema DEBE omitirlo de la corrida y listarlo en `data.skipped`; CUANDO no queda ningún caso para correr (o `--case` nombra uno que no existe), DEBE rechazar con código 2 y `code: no_cases`.
- R4 [N] CUANDO no existe el agente generado `bflow-eval-reviewer` para la herramienta del repo, `bflow eval reviewer` DEBE rechazar con código 2, `code: eval_agent_missing` y un motivo que diga correr `bflow render` y reiniciar la sesión.
- R5 [D] CUANDO los casos son válidos, el sistema DEBE crear la corrida, preparar el primer caso pendiente (worktree desacoplado en `head` y carpeta `out` vacía) y responder `next: spawn` de `bflow-eval-reviewer` con args `id` (nombre del caso), `base`, `worktree`, `out`, `craft` y, si el caso la declara, `spec`; `report` es `bflow eval reviewer report <caso> --verdict APPROVED|REJECTED` y `reason` dice correr `bflow eval reviewer` cuando el agente termine.

**Corrida**

- R6 [N] CUANDO hay un caso lanzado sin reporte, `bflow eval reviewer` (sin flags) DEBE responder el mismo spawn sin preparar otro caso ni recrear su worktree.
- R7 [N] CUANDO hay una corrida abierta y se pasa `--craft`, `--case` o `--from-fixes`, el sistema DEBE rechazar con código 2 y `code: eval_open`, nombrando la corrida y cómo seguirla o cancelarla.
- R8 [D] CUANDO llega `bflow eval reviewer report <caso>` del caso en curso y existe `<out>/review-map.md`, el sistema DEBE calificar el caso (R11-R12), guardar su resultado en la corrida con `verdict`, `note` y la hora del reporte, quitar su worktree y responder ok con el puntaje del caso. No aplica las reglas de formato ni de cobertura del review-map del flujo.
- R9 [N] CUANDO el report llega sin `<out>/review-map.md`, el sistema DEBE rechazar con código 2 y `code: review_map_missing`; CUANDO el caso no es el que está en curso, con `code: case_not_pending`; CUANDO no hay corrida abierta, con `code: eval_not_running`; CUANDO el veredicto no es APPROVED ni REJECTED, con `code: bad_verdict`.
- R10 [D] CUANDO `bflow eval reviewer` encuentra casos reportados sin tokens, DEBE sumarles las llamadas no finales del agente `eval-reviewer` de `.bflow/tasks/*/calls.jsonl` y `.bflow/metrics/calls.jsonl` con `ts` dentro de [spawn, report], el modelo más frecuente de esas llamadas y los segundos entre spawn y report; sin llamadas, `tokens_available: false` y el modelo configurado del reviewer.

**Puntaje**

- R11 [D] El sistema DEBE tomar como hallazgo cada referencia `ruta:N` o `ruta:N-M` del `--note` y de cada `.md` de `out`, salvo las líneas que contienen `caminos:`, `cubre:` o `el código responde` y las secciones `## Docs` y `## Pruebas sin ejecutar`; la ruta se normaliza a relativa al worktree con `/`. Un defecto cuenta como atrapado CUANDO algún hallazgo del mismo archivo intersecta [desde-5, hasta+5].
- R12 [D] Los hallazgos, deduplicados por `ruta:N` (o `ruta:N-M`), que no coinciden con ningún defecto DEBEN listarse como `false_positives` sin etiquetar.

**Cierre y limpieza**

- R13 [D] CUANDO no quedan casos pendientes y todos tienen tokens calculados, `bflow eval reviewer` DEBE escribir `.bflow/evals/<AAAA-MM-DD-HHMMSS>.json` (hora local de inicio de la corrida) con `craft.ref` (`""` = árbol), `craft.sha256`, `model`, `version` de bflow, cada caso y los totales; borrar la carpeta de la corrida y responder `action: done` con `atrapó X de Y defectos` en `reason`.
- R14 [D] CUANDO falla la preparación de un caso, o se corre `--abort`, el sistema DEBE quitar todos los worktrees de la corrida (`git worktree remove --force` y `git worktree prune`) y borrar su carpeta; `--abort` no escribe resultado y sin corrida abierta responde ok sin hacer nada.

**Dataset y versionado**

- R15 [D] CUANDO se corre `--from-fixes`, el sistema DEBE crear, por cada evento `start` con `data.fixes = X` cuyo caso no exista, `.bflow/evals/reviewer/<x>-fix-<hotfix>/case.yaml` (minúsculas) con `draft: true`, `source: X`, `hotfix`, `base` = `start_sha` de X, `head` = `sha` de `.bflow/tasks/X/check.json` (vacíos si no hay) y `defects: []`; no sobrescribe casos existentes ni corre nada; sin eventos responde ok con `data.created: []` y un aviso.
- R16 [N] CUANDO se crean una corrida o esqueletos, el sistema DEBE asegurar las líneas `!evals/` y `!evals/**` en `.bflow/.gitignore`; si git sigue ignorando `.bflow/evals/reviewer/`, DEBE agregar a `data.warnings` qué cambiar en el `.gitignore` raíz.
- R17 [N] El guard DEBE permitir a un subagente escribir `.md` bajo `.bflow/cache/evals/<corrida>/<caso>/out/` y permitir `git add` (sin `-f`) de rutas bajo `.bflow/evals/`; el resto de `.bflow/` sigue negado igual que hoy.
- R18 [N] `bflow render` DEBE generar `bflow-eval-reviewer` (Claude Code y OpenCode) solo si existe `.bflow/evals/reviewer/`, con el modelo y esfuerzo de `agents.reviewer`, el contrato del eval y sin oficio embebido; sin esa carpeta no lo genera ni lo marca como conflicto.
- R19 [D] El repo DEBE traer al menos 5 casos no borrador en `.bflow/evals/reviewer/`, sacados de escapes conocidos del propio repo, que pasen la validación de R1.
- R20 [D] La salida para agentes DEBE ser JSON compacto; el texto para personas usa color sin emojis. `docs/comandos.md` documenta el comando y `CHANGELOG.md` lo anuncia.

## Design

### Archivos

| Ruta | Qué |
|---|---|
| `.bflow/evals/reviewer/<caso>/case.yaml` | caso (versionado) |
| `.bflow/evals/<AAAA-MM-DD-HHMMSS>.json` | resultado (versionado) |
| `.bflow/cache/evals/<corrida>/run.json` | estado de la corrida abierta (ignorado) |
| `.bflow/cache/evals/<corrida>/craft.md` | oficio del reviewer del ref (o del árbol) |
| `.bflow/cache/evals/<corrida>/<caso>/wt/` | worktree desacoplado en `head` |
| `.bflow/cache/evals/<corrida>/<caso>/out/` | donde el agente escribe `review-map.md` y otros `.md` |

`<corrida>` = hora local de inicio `AAAA-MM-DD-HHMMSS`; es también el nombre del resultado. Solo hay una corrida abierta: la carpeta de `.bflow/cache/evals/` con `run.json`.

`case.yaml`:

```yaml
source: GH-159          # tarea donde se aprobó el defecto (informativo)
hotfix: GH-160          # opcional
base: 0edfbea           # SHA (7-40 hex) o ref; se resuelve al validar
head: 42249aa
spec: specs/GH-159-.../spec.md   # opcional, relativa al worktree
draft: false            # true = esqueleto, se omite
defects:
  - file: internal/engine/repro.go
    lines: "120-128"    # "N" o "N-M"
    what: el escape rechazado se reabre
```

### Paquete `internal/eval` (nuevo; no importa adaptadores)

```go
package eval

const Tolerance = 5          // líneas de holgura del puntaje
const AgentName = "eval-reviewer" // nombre sin prefijo; subagente flow.SubagentPrefix+AgentName
const CraftPath = "internal/agents/craft/reviewer.md"

type Defect struct {
    File  string `yaml:"file" json:"file"`
    Lines string `yaml:"lines" json:"lines"`
    What  string `yaml:"what" json:"what"`
}
type Case struct {
    Name    string   `yaml:"-" json:"case"`
    Source  string   `yaml:"source" json:"source,omitempty"`
    Hotfix  string   `yaml:"hotfix,omitempty" json:"hotfix,omitempty"`
    Base    string   `yaml:"base" json:"base"`
    Head    string   `yaml:"head" json:"head"`
    Spec    string   `yaml:"spec,omitempty" json:"spec,omitempty"`
    Draft   bool     `yaml:"draft,omitempty" json:"-"`
    Defects []Defect `yaml:"defects" json:"defects"`
}

// Rejection es un rechazo de reglas (código 2): Code y Reason van al envelope.
type Rejection struct{ Code, Reason string }
func (r *Rejection) Error() string

// Git son las operaciones que el eval pide a git; las implementa adapters/vcs/git.
type Git interface {
    ResolveCommit(ctx context.Context, ref string) (string, error) // SHA completo; error si no es commit
    ShowFile(ctx context.Context, ref, path string) ([]byte, error)
    WorktreeAdd(ctx context.Context, dir, sha string) error        // --detach
    WorktreeRemove(ctx context.Context, dir string) error          // --force, luego prune
    IsIgnored(ctx context.Context, path string) (bool, error)      // git check-ignore -q
}

func LoadCases(root string, names []string) (cases []Case, skipped []string, err error)
func (c Case) Problems(ctx context.Context, g Git) []string
func ParseLines(s string) (from, to int, err error)

type Finding struct{ File string; From, To int }
func Findings(note string, docs map[string]string, worktree string) []Finding // docs: nombre .md → contenido
type Score struct {
    Caught         []Defect `json:"caught"`
    Missed         []Defect `json:"missed"`
    FalsePositives []string `json:"false_positives"`
}
func ScoreCase(defects []Defect, found []Finding) Score

func Tokens(bflowDir string, from, to time.Time) (u metrics.Usage, model string, ok bool)

type Opts struct{ Craft string; Cases []string; Abort bool }
type Spawn struct{ Case string; Args map[string]string; Report string }
type Step struct {
    Spawn    *Spawn   // caso a lanzar; nil si terminó
    Result   *Result  // al cerrar
    Path     string   // ruta del resultado, relativa a la raíz
    Skipped  []string
    Warnings []string
}
type Runner struct {
    Root    string
    Git     Git
    Now     func() time.Time
    Model   string // modelo configurado de agents.reviewer (respaldo de R10)
    Version string
}
func (r *Runner) Next(ctx context.Context, o Opts) (Step, error)
func (r *Runner) Report(ctx context.Context, caseName, verdict, note string) (CaseResult, error)
func FromFixes(root string, log []store.Entry) (created []string, warnings []string, err error)

type CaseResult struct {
    Case            string        `json:"case"`
    Verdict         string        `json:"verdict"`
    Note            string        `json:"note,omitempty"`
    Score
    Spawned         time.Time     `json:"spawned"`
    Reported        time.Time     `json:"reported"`
    Seconds         int64         `json:"seconds"`
    Model           string        `json:"model"`
    Tokens          metrics.Usage `json:"tokens"`
    TokensAvailable bool          `json:"tokens_available"`
}
// run.json guarda cada CaseResult junto con tokens_done (tokens ya sumados, R10).
type Result struct {
    Kind    string `json:"kind"` // "reviewer"
    Run     string `json:"run"`
    Version string `json:"version"`
    Craft   struct{ Ref, SHA256 string } `json:"craft"`
    Model   string       `json:"model"`
    Cases   []CaseResult `json:"cases"`
    Totals  struct {
        Defects, Caught, Missed, FalsePositives int
        Tokens  metrics.Usage
        Seconds int64
    } `json:"totals"`
}
```

Las claves JSON de `Craft` y `Totals` van en snake_case (`ref`, `sha256`, `defects`, `caught`, `missed`, `false_positives`, `tokens`, `seconds`). `run.json` guarda `Opts`, la lista de casos, el índice del caso en curso, `spawned` y los `CaseResult` (con un `tokens_done`).

### Flujo de `Runner.Next`

1. `Abort`: si hay corrida, quita worktrees y carpeta (R14); ok.
2. Corrida abierta: con `Craft`/`Cases` no vacíos → `eval_open` (R7). Calcula tokens de los casos reportados sin `tokens_done` (R10). Caso en curso sin reporte → mismo spawn (R6). Siguiente pendiente → prepara y spawn. Ninguno → escribe resultado y borra la corrida (R13).
3. Sin corrida: `LoadCases` (R1, R3) → craft: `Craft == ""` lee `CraftPath` del árbol; si no, `ResolveCommit` + `ShowFile` (R2) → escribe `craft.md`, `sha256` del contenido → `.gitignore` (R16) → prepara el primer caso.

Preparar = `WorktreeAdd(<caso>/wt, head)` + `MkdirAll(<caso>/out)`; si falla, R14 y devuelve el error. `Report` quita el worktree antes de responder; si `WorktreeRemove` falla, el resultado se guarda igual y el error va a `warnings` (se reintenta al cerrar).

### Puntaje

- Referencia: `(?:^|[\s(\x60"'])((?:[A-Za-z]:)?[\w./\\-]+\.\w+):(\d+)(?:-(\d+))?` sobre cada línea; las rutas absolutas que empiezan por el worktree se recortan (sin distinguir mayúsculas en Windows); `\` → `/`; `./` inicial fuera. Referencias fuera del worktree se descartan.
- Exclusiones de R11 por línea y por sección (un encabezado `## Docs` o `## Pruebas sin ejecutar` excluye hasta el siguiente encabezado de nivel igual o menor).
- Coincidencia: misma ruta exacta y `[f.From, f.To] ∩ [d.from-5, d.to+5] ≠ ∅`.

### Agente `bflow-eval-reviewer`

`agents.EvalReviewerSpec(conf config.AgentConf) Spec` arma el spec: descripción "Revisa un caso del eval del reviewer y escribe su review-map. Lo lanza la sesión principal cuando bflow eval lo pide; no lo invoques por tu cuenta.", herramientas Read, Write, Bash, modelo y esfuerzo de `agents.reviewer` (o del catálogo). Cuerpo (`evalContract()`):

- Recibes `id` (un caso del eval, no una tarea), `base`, `worktree` (el código del caso), `out`, `craft` y, si existe, `spec` (relativa al worktree).
- Antes de empezar, lee `craft`: es tu oficio; síguelo como si estuviera aquí.
- El diff es `git -C <worktree> diff <base>...HEAD`; usa rutas absolutas.
- `id` no es una tarea: no corras `bflow check`, `bflow show` ni `bflow report`. Da el check por verde; los reportes de otros agentes no existen.
- Los reportes que pida tu oficio (`reports/<nombre>.md`) van en `<out>/<nombre>.md`. Fuera de `out` no escribes nada.
- Al terminar: `bflow eval reviewer report <id> --verdict APPROVED|REJECTED`; con REJECTED agrega `--note`. Código 2: lee el motivo y corrige. Tu respuesta final es solo esa salida.

`rendercmds.go` agrega ese spec a los de la herramienta si existe `.bflow/evals/reviewer/` (R18). `bflow eval reviewer` comprueba con el mismo plan de render que el archivo del subagente exista (R4).

### CLI (`internal/cli/evalcmds.go`)

- `eval reviewer` — flags `--craft <ref>`, `--case <nombre>` (repetible, `multi`), `--abort`, `--from-fixes`. Arma `eval.Runner` con `adapters/vcs/git`, el modelo de `agents.Models(...)["reviewer"]` y `c.Version`. Spawn → `output.Next{Action: spawn, Agents: [{Agent: "eval-reviewer", Subagent: "bflow-eval-reviewer", Args, Report}], Reason}`; cierre → `Action: done`; `data` con `run`, `case`, `skipped`, `warnings`, `result` (ruta y totales).
- `eval reviewer report <caso> --verdict V [--note t]` → `data.score`.
- `*eval.Rejection` → `output.Rejected(code, reason)`; otros errores → `output.Fail`.

### Guard (`internal/guard/guard.go`)

- `agentArtifact` acepta además `.bflow/cache/evals/<corrida>/<caso>/out/<nombre>.md` (5 segmentos bajo `.bflow`, sin `..`, extensión `.md`).
- En `git add`, una ruta bajo `.bflow/evals/` no marca `bad`; `-f`/`--force` sigue negado.

### Seguridad

- Nombres de caso: `^[a-z0-9][a-z0-9._-]*$` sin `..` (son carpetas); `--case` con otro formato → `no_cases`.
- Rutas de defectos y `spec`: relativas, sin `..`, sin unidad ni `/` inicial (R1).
- Refs: se rechazan los que empiezan con `-`; a git se pasan después de `--end-of-options` o como `<ref>^{commit}`; nunca por shell.
- Worktrees y salidas solo bajo `.bflow/cache/evals/`; el borrado comprueba que la ruta esté dentro de esa carpeta antes de `RemoveAll`.
- `note`, `what` y el contenido de `out` son datos de terceros: se guardan tal cual en JSON, no se ejecutan ni se interpolan en comandos.
- Sin secretos: el resultado no guarda rutas absolutas (los worktrees se recortan) ni el texto del oficio, solo su hash.

### Descartado

- `bflow` lanzando `claude -p`/`opencode run` (discovery). Juez LLM (discovery). Minar commits `fix(GH-N)` (discovery).
- Agente regenerado por corrida; tarea sintética por caso; leer solo `## Seguimiento`; aplicar el formato del flujo al eval (ver Brief).

## Tasks

- [ ] T1 Contrato: paquete `internal/eval` con los tipos y firmas del Design (cuerpos que devuelven cero o `errors.New("sin implementar")`), métodos vacíos en `adapters/vcs/git`, `agents.EvalReviewerSpec` vacío y estas pruebas, que fallan:
  - `internal/eval/case_test.go`: `TestLoadCasesRejectsMalformed` (R1), `TestLoadCasesSkipsDrafts` (R3), `TestLoadCasesNoCases` (R3), `TestParseLines` (R1).
  - `internal/eval/craft_test.go`: `TestCraftFromTree`, `TestCraftUnknownRef`, `TestCraftMissingAtRef`, `TestCraftRefWithDash` (R2).
  - `internal/eval/score_test.go`: `TestScoreCatchesWithinTolerance`, `TestScoreMissesOutsideTolerance` (R11), `TestFindingsSkipsExcluded` (R11), `TestFindingsTrimsWorktreePrefix` (R11), `TestScoreFalsePositivesDeduped` (R12).
  - `internal/eval/run_test.go`: `TestNextSpawnsFirstCase` (R5), `TestNextRepeatsPendingSpawn` (R6), `TestNextOpenRunRejectsFlags` (R7), `TestReportScoresAndRemovesWorktree` (R8), `TestReportRejections` (R9), `TestTokensFromCallsWindow` (R10), `TestNextWritesResult` (R13), `TestAbortCleansUp`, `TestPrepareFailureCleansUp` (R14), `TestGitignoreExceptions` (R16).
  - `internal/eval/fixes_test.go`: `TestFromFixesSkeletons`, `TestFromFixesKeepsExisting`, `TestFromFixesNoEvents` (R15).
  - `internal/guard/guard_test.go`: `TestEvalOutWriteAllowed`, `TestGitAddEvalsAllowed` (R17).
  - `internal/agents/agents_test.go`: `TestEvalReviewerSpec` (R18); `internal/cli/eval_test.go`: `TestRenderEvalAgentOnlyWithCases` (R18), `TestEvalAgentMissing` (R4).
  - `cmd/bflow/e2e_eval_test.go`: `TestE2EEvalReviewer` (repo git real: un caso, spawn, report, cierre y JSON; R5, R8, R13, R20).
- [ ] T2 Casos, oficio y git: `LoadCases`, `Problems`, `ParseLines`, lectura del oficio del árbol o del ref, y `ResolveCommit`, `ShowFile`, `WorktreeAdd`, `WorktreeRemove`, `IsIgnored` en `adapters/vcs/git`. (R1, R2, R3)
- [ ] T3 Puntaje: `Findings` y `ScoreCase` con exclusiones, normalización de rutas y tolerancia. (R11, R12)
- [ ] T4 Corrida: `Runner.Next`, `Runner.Report`, `Tokens`, `run.json`, resultado, `--abort`, limpieza, `.bflow/.gitignore` y comandos `eval reviewer` y `eval reviewer report` en `internal/cli/evalcmds.go` (JSON compacto, texto con color sin emojis). (R5-R10, R13, R14, R16, R20)
- [ ] T5 Agente y guard: `agents.EvalReviewerSpec` y su contrato, render condicional en `rendercmds.go`, chequeo `eval_agent_missing`, excepciones del guard. (R4, R17, R18)
- [ ] T6 `--from-fixes`: esqueletos desde los eventos `start` con `fixes`, `start_sha` y `check.json`. (R15)
- [ ] T7 Dataset y docs: 5 o más casos curados en `.bflow/evals/reviewer/` (escapes conocidos: GH-159, #108 y otros con `fix(GH-…)` posterior a su aprobación), cada uno con `base`, `head` y defectos archivo:rango verificados con `git show`; `.gitignore` raíz a `.bflow/*` + `!.bflow/evals/`; `bflow render` para generar el agente; `docs/comandos.md` y `CHANGELOG.md`. (R16, R19, R20)
