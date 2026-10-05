# GH-51 · Tokens de la sesión principal: se cargan a la tarea equivocada o se pierden

## Brief

**Objetivo:** cada llamada al modelo se carga a la tarea que su sesión conducía en ese momento; si no conducía ninguna, va a "sin tarea" y no se pierde.

**Entra**
- El guard (PreToolUse) anota una *marca* (sesión → tarea, con su hora) en `.bflow/cache/sessions.jsonl` cuando ve un comando `bflow … <ID>` de una tarea existente o el lanzamiento de un subagente `bflow-*` cuyo prompt trae `"id":"<ID>"`.
- `hook tokens` deja de usar `e.Active()`: reparte cada llamada según las marcas de su sesión y su hora. Lo que no tiene marca va a `.bflow/metrics/calls.jsonl` (mismo formato que `calls.jsonl` de una tarea).
- El cursor de tokens se guarda solo si todas las filas se escribieron.
- `bflow stats` (sin ID) y `bflow watch` muestran una línea "sin tarea" si es mayor que 0.
- Ajustes: el matcher de PreToolUse de Claude suma `Task|Agent`; el plugin de OpenCode manda `task` al guard.

**No entra**
- Registrar tokens a mitad de turno (GH-84).
- Corregir datos ya mal cargados (GH-19 y otros): sin migración.

**Decisiones nuevas**
- [N] Un subagente se reparte primero por **sus propias** marcas (los agentes de bflow corren `bflow show <ID>` al empezar); solo si no tiene, por las de su sesión madre a la hora de cada llamada. Descartado: solo la marca de la madre al lanzarlo; con dos lanzamientos en paralelo (GH-X y GH-Y) el primero quedaría en GH-Y.
- [N] La llave de sesión de un subagente de Claude es `<session_id>:<agent_id>` (en Claude comparte `session_id` con la madre). Descartado: marcar solo con `session_id`, que mezclaría las marcas del subagente con las de la sesión principal.
- [N] Una marca a una tarea que ya no está en el store (drop) cuenta como "sin tarea". Descartado: crear entradas en el log de una tarea inexistente.
- [N] Solo marcan las acciones que el guard permite; un comando rechazado no ocurrió.
- [N] El resumen global es `bflow stats` (el discovery dice `bflow metrics`, que no existe).

**Riesgos**
- La llamada que corre `bflow next <ID>` es anterior a su marca: va a la tarea previa o a "sin tarea". Igual tras `/bflow <ID>` hasta el primer comando bflow (aceptado en el discovery).
- Instalaciones existentes necesitan `bflow render`/install para el matcher `Task|Agent`; sin él, los subagentes que no corren comandos bflow caen a la marca de la madre, que sigue funcionando.
- Si falla una escritura a mitad del Stop, el cursor no avanza y las filas ya escritas se cuentan otra vez en el siguiente Stop (duplicado raro, preferible a perder).
- Una sesión con más de 7 días sin tocar bflow pierde sus marcas: sus llamadas nuevas van a "sin tarea".
- Cambia la interfaz `AgentAdapter.TokenSource` y `metrics.TokenSource`: fakes de prueba.

**Tamaño:** L (guard, dos adaptadores, metrics, hook tokens, stats, watch, plugin y settings; ~600 líneas con pruebas). Cabe en una tarea: todo es un solo camino de datos (marca → reparto → vista).

## Discovery

El Stop carga las llamadas de cualquier sesión a `e.Active()` y guarda el cursor antes de saber si hay tarea, así que unas van a la tarea equivocada y otras se pierden. Decidido: el guard anota sesión → tarea; el Stop reparte por llamada y hora; los subagentes siguen a su madre; lo que no tiene dueño va a un registro "sin tarea" del repo; el cursor avanza solo tras guardar; "sin tarea" se ve en el resumen global y en watch. Fuera: tokens a mitad de turno (GH-84) y migrar datos viejos.

## Requirements

- **R1** [D] CUANDO el guard permite un comando Bash con un segmento que invoca a bflow (`guard.BflowSubcommand` ≠ "") y uno de sus argumentos sin guion, en mayúsculas, pasa `store.ValidID` y existe como `.bflow/tasks/<ID>/`, el sistema DEBE anotar una marca `{ts, session, id}` con el primer argumento que cumpla, para la sesión de la acción.
- **R2** [D] CUANDO el guard ve el lanzamiento de un subagente cuyo tipo empieza con `bflow-` (Claude: herramienta `Task` o `Agent`, `tool_input.subagent_type`; OpenCode: herramienta `task`, `args.subagent_type`) y su prompt contiene `"id":"<ID>"` de una tarea existente, el sistema DEBE anotar la marca para la sesión que lo lanza. El lanzamiento siempre se permite.
- **R3** [D] La anotación NO DEBE usar red ni tracker ni imprimir nada; SI falla (lock ocupado más de 1 s, error de disco), el guard DEBE seguir como si no hubiera marca.
- **R4** [D] La llave de sesión DEBE ser: Claude principal `session_id`; Claude subagente `session_id + ":" + agent_id` (madre: `session_id`); OpenCode `sessionID` (madre: `parentID` de la primera línea del archivo de uso).
- **R5** [D] CUANDO `hook tokens` lee llamadas de una sesión principal, cada llamada DEBE ir a la tarea de la última marca de esa sesión con `ts` ≤ hora de la llamada; sin marca previa, a "sin tarea". El sistema NO DEBE usar `e.Active()` para repartir.
- **R6** [N] CUANDO lee llamadas de un subagente, cada llamada DEBE ir a: la última marca propia con `ts` ≤ hora de la llamada; si no hay, la primera marca propia; si no tiene marcas, la última marca de la madre con `ts` ≤ hora de la llamada; si tampoco, "sin tarea".
- **R7** [N] SI la marca elegida apunta a una tarea que `e.Store.Load` no encuentra, la llamada DEBE ir a "sin tarea".
- **R8** [D] Las llamadas de una tarea DEBEN registrarse como hoy (`addTokens`: entradas `tokens` en el log, `calls.jsonl` de la tarea, caché de tokens), agrupadas por tarea dentro del mismo Stop.
- **R9** [D] Las llamadas "sin tarea" DEBEN agregarse a `.bflow/metrics/calls.jsonl` como `metrics.Call` con `Phase` vacía, y NO DEBEN escribir en `log.jsonl` ni en la caché de tokens de ninguna tarea.
- **R10** [D] El cursor (`.bflow/cache/tokens-cursor.json`) DEBE guardarse solo después de escribir todas las filas del Stop; SI alguna escritura falla, el sistema NO DEBE guardarlo y DEBE salir en silencio.
- **R11** [D] CUANDO corre `hook tokens` de una sesión principal, el sistema DEBE borrar de `sessions.jsonl` las marcas con más de 7 días, bajo el lock de marcas.
- **R12** [D] CUANDO `bflow stats` corre sin ID y "sin tarea" suma más de 0, DEBE mostrar una línea `sin tarea  <metrics.Summary>` al final y `data.unassigned` (`metrics.Usage`); con 0 no muestra nada.
- **R13** [D] CUANDO `bflow watch` dibuja el panel y "sin tarea" suma más de 0, DEBE mostrar `sin tarea  <metrics.Summary>` antes de la lista "otras", haya o no tarea activa.
- **R14** [N] Los ajustes de Claude (`adapters/claude/settings.json`) DEBEN incluir `Task|Agent` en el matcher de PreToolUse y el plugin de OpenCode DEBE mandar `task` al guard (por la vía sin espera, como `read`).
- **R15** [D] Con estos cambios, una sesión que nunca corrió un comando bflow con el ID de una tarea NO DEBE sumarle tokens, aunque sea la única en curso o la de la rama actual.

## Design

### Marcas (`internal/metrics/marks.go`, nuevo)

```go
// Mark dice que una sesión condujo una tarea desde TS.
type Mark struct {
    TS      time.Time `json:"ts"`
    Session string    `json:"session"`
    ID      string    `json:"id"`
}
func MarksPath(bflowDir string) string          // <bflowDir>/cache/sessions.jsonl
func UnassignedPath(bflowDir string) string     // <bflowDir>/metrics/calls.jsonl
func AppendMark(path string, m Mark) error      // lock path+".lock" (1 s, stale 1 min), una línea, O_APPEND
func ReadMarks(path string) []Mark              // ignora líneas rotas; archivo ausente = nil
func PruneMarks(path string, olderThan time.Duration, now time.Time) error // bajo el mismo lock, WriteAtomic
// TaskFor aplica R5/R6: "" = sin tarea.
func TaskFor(marks []Mark, src TokenSource, ts time.Time) string
```

`metrics.TokenSource` pasa a `struct{ Path, Agent, Session, Parent string }` (Parent "" = sesión principal). `UnassignedLabel = "sin tarea"` para las vistas.

### Guard

- `guard.Action` gana `Session string` (llave R4) y `Target string` (tipo de subagente lanzado); el prompt va en `Command`. Nueva constante `guard.Spawn = "spawn"`; `Evaluate` la permite (cae en el `return allow`).
- `guard.BflowArgs(seg string) []string`: argumentos sin guion tras el subcomando de un segmento bflow (misma lógica de `BflowSubcommand`); nil si no es bflow.
- Claude `ParsePreToolUse`: lee `session_id`, `tool_input.subagent_type` y `tool_input.prompt`; `Task`/`Agent` → `Action{Tool: Spawn, Target, Command: prompt}`.
- OpenCode `ParseActions`: `Session = sessionID`; `task` → `Spawn` con `args.subagent_type` y `args.prompt`.
- `cli.markSession(c *Ctx, cfg *config.Config, acts []guard.Action)` en `hookcmds.go`: se llama en `runGuard` solo cuando todas las acciones se permitieron; busca el ID (R1/R2) con `os.Stat` de `.bflow/tasks/<ID>`; regex del prompt `"id"\s*:\s*"([^"]+)"`. Sin `Session` no marca.

### Hook tokens (`internal/cli/metricscmds.go`)

- `AgentAdapter.TokenSource(raw []byte) metrics.TokenSource` (antes `(path, agent string)`); Claude llena `Session`/`Parent` según R4. OpenCode `TokenSources` llena `Session` con el `sessionID` de la primera línea y `Parent` con su `parentID`.
- `tokensFrom`: lee las fuentes (como hoy), lee las marcas una vez, agrupa cada muestra en `map[id][]Sample` por fuente con `TaskFor` + `Store.Load` (R7); escribe cada grupo con `addTokens` (ahora devuelve `error` además de lo que devuelve) y los de "" con `addUnassigned(e, tool, run, agent string, samples) error`; si todo salió bien, prune de caché y de marcas (solo principal), y guarda el cursor (R10). `notifyActive` se queda igual.
- `addUnassigned` escribe con `store.EnsureDirFor` + append bajo el lock del cursor (que ya tiene tomado).

### Vistas

- `metrics.UnassignedUsage(bflowDir string) metrics.Usage`: `ParseCalls` + `Runs` y suma de `Total`.
- `runStats` sin ID: R12. `watchData.Unassigned metrics.Usage`, llenado en `readWatchIn` también sin tarea activa; `renderWatch` lo dibuja con `dimLabel` antes de `watchOthers` (R13).

### Descartado

- Escanear el transcript en el Stop para encontrar comandos bflow (discovery).
- Repartir por turno en vez de por llamada (discovery).
- Dejar las llamadas pendientes hasta el siguiente Stop con tarea (discovery).

### Seguridad

- Entradas de terceros: `session_id`, `agent_id`, prompt y comando vienen del stdin del hook. El ID se valida con `store.ValidID` antes de tocar el disco (sin `../`); la sesión solo se escribe dentro de JSON con `json.Marshal`, nunca en una ruta.
- El prompt no se guarda: solo el ID extraído.
- Sin authz nueva: todo es local a `.bflow/`, que ya es ignorado por git.
- Errores: todo camino de hook sale en silencio (código 0, sin salida); el guard nunca bloquea por una marca.

## Tasks

- [x] **T1 Contrato** (R1-R15): tipos y firmas del Design (`Mark`, `MarksPath`, `UnassignedPath`, `AppendMark`, `ReadMarks`, `PruneMarks`, `TaskFor`, `UnassignedUsage`, `TokenSource` con `Session`/`Parent`, `guard.Spawn`, `Action.Session`/`Target`, `BflowArgs`, nueva firma de `AgentAdapter.TokenSource`) con cuerpos mínimos que compilan, y las pruebas que fallan:
  - `internal/guard`: `TestBflowArgs`.
  - `internal/adapters/agent/claude`: `TestParsePreToolUseSession`, `TestParsePreToolUseSpawn`, `TestTokenSourceSession`.
  - `internal/adapters/agent/opencode`: `TestParseActionsSpawn`, `TestTokenSourcesSession`.
  - `internal/metrics`: `TestTaskForMain`, `TestTaskForSubagent`, `TestMarksAppendPrune`, `TestUnassignedUsage`.
  - `cmd/bflow` (e2e): `TestGuardMarksSession`, `TestHookTokensOnlyMarkedSession`, `TestHookTokensUnassignedWithoutActive`, `TestHookTokensSplitByTime`, `TestHookTokensSubagentFollowsParent`, `TestHookTokensDroppedTaskUnassigned`, `TestHookTokensCursorHeldOnWriteError`, `TestStatsUnassigned`, `TestWatchUnassigned`.
- [ ] **T2 Marcas en metrics** (R6, R7, R11): `marks.go` completo (append con lock, lectura tolerante, prune, `TaskFor`).
- [ ] **T3 Guard y adaptadores** (R1-R4, R14): parseo de sesión y lanzamientos en Claude y OpenCode, `BflowArgs`, `markSession` en `runGuard`; matcher `Task|Agent` en `adapters/claude/settings.json` y `task` en `adapters/opencode/plugin.js`.
- [ ] **T4 Reparto en hook tokens** (R4-R10, R15): nueva `TokenSource` en ambos adaptadores, `tokensFrom` sin `e.Active()`, `addTokens` con error, `addUnassigned`, cursor tras escribir, prune de marcas en Stop principal.
- [ ] **T5 Vistas** (R12, R13): `UnassignedUsage`, línea y `data.unassigned` en `stats` global, línea en `watch`.
