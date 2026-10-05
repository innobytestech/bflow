# GH-61 · Lecturas de todos los agentes y relectura por archivo en stats

## Brief

**Objetivo:** registrar cada Read de cualquier agente y de la sesión principal en `reads-all.jsonl` y mostrar en `bflow stats <ID> --reads` cuánto se relee entre agentes. La tarea solo mide: no cambia lo que reciben los agentes.

**Entra**
- `reads-all.jsonl` por tarea: `ts`, `agent`, `phase`, `path` relativo, `partial`, `tool` (claude | opencode).
- Claude Code: `bflow guard --reads` ya ve todos los Read. Lo que cambia es que ahora se registran todos, no solo los del reviewer.
- OpenCode: el plugin manda todos los `read` al guard, pero solo espera la respuesta en los del reviewer.
- `bflow stats <ID> --reads` (texto y `--json` → `data.reads`), que se puede combinar con `--calls`.
- Aviso "sin registro de lecturas" en tareas viejas.

**No entra:** `bflow context`, índice de símbolos, Grep/Glob, costo en tokens por archivo, sugerencias de `doctor`, panel web.

**Decisiones del discovery** (resumen): archivo nuevo en vez de ampliar `reads.jsonl`, así `review_incomplete` y el walkthrough no se tocan. Solo se registra Read. Un Read parcial cuenta como lectura y se marca. Hay dos métricas: % de lecturas que releen un archivo que otro agente ya leyó, y % de archivos leídos por 2 o más agentes.

**Decisiones nuevas**
- [N] Si todas las acciones del hook son Read, el guard no las evalúa (`Evaluate` siempre permite Read): carga la config, resuelve la tarea con `Active` y la fase con `Store.Load`, y escribe. Alternativa descartada: conservar `onlyForeignReads`, que impediría medir a los demás agentes.
- [N] Una lectura de un subagente sin nombre (`agent_type` vacío pero con `agent_id`) se guarda como `agent: "unknown"`. Alternativa descartada: atribuirla a `main`, lo que inflaría la sesión principal.
- [N] La tabla de texto muestra las 30 filas de arriba y una línea "y K archivos más (--json para todos)". `--json` trae todas. Alternativa descartada: imprimir todas las filas (más de 200 en una tarea grande).
- [N] La sección de lecturas se pinta como `--calls`: línea de stats y debajo el bloque. Con `--calls --reads` van primero las llamadas y luego las lecturas.
- [N] Que el archivo no exista y que no tenga líneas válidas se tratan igual: `available: false` y el aviso.

**Riesgos**
- Latencia en Claude Code: antes, el Read de otro agente salía sin leer la config. Ahora cada Read cuesta `config.Load`, leer la lista de tareas y `git` para saber la rama actual. No se consulta el tracker. Hay que medirlo en Windows con antivirus (pendiente conocido).
- Hooks en paralelo de varios subagentes escriben al mismo archivo. Cada lectura se agrega con un solo `Write` en modo append, y si una línea queda corrupta se salta al leer.
- El plugin de OpenCode cambia, así que los repos tienen que volver a correr `bflow render`.

**Tamaño:** M. Son unos 8 archivos de Go, el plugin y pruebas. No hay migraciones.

## Discovery

Va un archivo nuevo, `reads-all.jsonl`; `reads.jsonl` y `review_incomplete` no cambian. Solo se registra Read; un Read parcial cuenta y queda con `partial: true`. Las rutas se guardan relativas al repo con `review.Normalize` y lo que cae fuera del repo se descarta. Si no hay tarea activa o hay varias sin forma de elegir, no se registra nada. Nunca se bloquea ni se falla. OpenCode lanza sin esperar los read que no son del reviewer. `stats --reads` muestra un resumen con dos métricas de relectura y una tabla por archivo. El costo en tokens por archivo queda fuera.

## Requirements

- **R1** [D] CUANDO cualquier agente o la sesión principal hace un Read en Claude Code u OpenCode y hay una tarea activa, el sistema DEBE agregar una línea a `.bflow/tasks/<ID>/reads-all.jsonl` con `ts`, `agent`, `phase`, `path`, `partial` (se omite si es false) y `tool`.
- **R2** [D] El campo `agent` DEBE guardarse sin el prefijo `bflow-`. Para la sesión principal vale `metrics.MainSession` (`"main"`). Un subagente que no es de bflow conserva el nombre que le da la herramienta.
- **R3** [N] CUANDO la acción viene de un subagente sin nombre (`Subagent` true y `Agent` vacío), el sistema DEBE guardar `agent: "unknown"`.
- **R4** [D] `path` DEBE ser la salida de `review.Normalize` (relativa al repo, con `/`). CUANDO la ruta cae fuera del repo, el sistema NO DEBE registrar la lectura. Nunca se guarda una ruta absoluta.
- **R5** [D] `phase` DEBE ser la fase de la tarea en el store local en ese momento. El hook NO DEBE consultar el tracker ni llamar a `Engine.Status`.
- **R6** [D] CUANDO no hay tarea activa o `Active` devuelve error (por ejemplo, ambigua), el sistema NO DEBE registrar nada.
- **R7** [D] CUANDO el Read trae `offset` o `limit` con un valor distinto de null, el sistema DEBE guardar `partial: true`. Esto aplica a Claude Code (`tool_input.offset/limit`) y a OpenCode (`args.offset/limit`).
- **R8** [D] `tool` DEBE valer `claude` cuando el guard corre sin `--tool` y, si no, el valor de `--tool` (por ejemplo `opencode`).
- **R9** [D] Registrar lecturas NUNCA DEBE bloquear ni cambiar la salida del guard: ante cualquier error, el guard permite en silencio (exit 0, sin stdout ni stderr).
- **R10** [D] `reads.jsonl` y su contenido NO DEBEN cambiar: el reviewer en quality sigue escribiendo ahí exactamente lo de hoy (Read, Bash, Edit, Write). Sus Read también van a `reads-all.jsonl`.
- **R11** [D] Grep y Glob NO DEBEN registrarse, y el matcher de `adapters/claude/settings.json` no cambia.
- **R12** [D] CUANDO OpenCode ejecuta un `read` fuera de la subsesión `bflow-reviewer`, el plugin DEBE lanzar `bflow guard --tool opencode --reads` sin esperar a que termine. En el read del reviewer y en las demás herramientas DEBE esperar como hoy.
- **R13** [D] CUANDO se corre `bflow stats <ID> --reads`, el sistema DEBE mostrar la línea de stats, un resumen (archivos únicos, lecturas totales, lecturas parciales, relecturas entre agentes con su %, archivos compartidos con su %) y una tabla con ruta, lecturas, parciales, número de agentes y sus nombres.
- **R14** [D] Una lectura cuenta como relectura entre agentes CUANDO otro agente leyó la misma ruta antes en la misma tarea, en orden de `ts` estable (si empatan, manda el orden del archivo). El % de relectura es relecturas / lecturas totales. El % de archivos compartidos es archivos con 2 o más agentes / archivos únicos. Los dos se redondean a 1 decimal y valen 0 si no hay lecturas.
- **R15** [D] Las filas DEBEN ir ordenadas por lecturas (desc), luego por número de agentes (desc) y luego por ruta (asc). Los nombres de agentes dentro de cada fila van en orden alfabético.
- **R16** [N] En texto, el sistema DEBE mostrar como máximo 30 filas. Si hay más, cierra con "y K archivos más (--json para todos)".
- **R17** [D] CUANDO se usa `--json --reads`, el sistema DEBE agregar `data.reads` con el resumen y todas las filas. `data.stats` sigue igual.
- **R18** [D] CUANDO no se pasa `--reads`, la salida de `stats` (texto y JSON, con o sin `--calls`) NO DEBE cambiar.
- **R19** [D/N] CUANDO `reads-all.jsonl` no existe o no tiene líneas válidas, el sistema DEBE mostrar "sin registro de lecturas" en texto y `data.reads.available: false` en JSON, con código OK.
- **R20** [D] Al leer `reads-all.jsonl`, el sistema DEBE saltarse las líneas inválidas.
- **R21** [D] CUANDO se pasan `--calls` y `--reads` juntos, el sistema DEBE mostrar los dos bloques: en texto, llamadas y luego lecturas; en JSON, `data.calls` y `data.reads`.
- **R22** [D] CUANDO se usa `--reads` sin ID, el sistema DEBE fallar con `usage`, igual que `--calls`. CUANDO el ID no existe, el error es el mismo que da `stats` hoy (`not_found`).

## Design

### Datos: `internal/metrics/reads.go` (nuevo, lógica pura)

```go
const ReadsAllFile = "reads-all.jsonl"
const UnknownAgent = "unknown" // subagente sin nombre (R3)

// ReadEvent es una línea de reads-all.jsonl.
type ReadEvent struct {
    TS      time.Time `json:"ts"`
    Agent   string    `json:"agent"`             // MainSession, UnknownAgent o nombre sin "bflow-"
    Phase   string    `json:"phase"`             // flow.Phase como texto
    Path    string    `json:"path"`              // relativo al repo, con /
    Partial bool      `json:"partial,omitempty"`
    Tool    string    `json:"tool"`              // "claude" | "opencode"
}

func ParseReadEvents(r io.Reader) []ReadEvent // salta líneas inválidas y las que no tienen path (R20)

type ReadRow struct {
    Path    string   `json:"path"`
    Reads   int      `json:"reads"`
    Partial int      `json:"partial"`
    Agents  []string `json:"agents"` // distintos, en orden alfabético; el número es len(Agents)
}

type ReadsSummary struct {
    Available     bool      `json:"available"`
    Files         int       `json:"files"`
    Reads         int       `json:"reads"`
    Partial       int       `json:"partial"`
    CrossRereads  int       `json:"cross_rereads"`
    CrossPct      float64   `json:"cross_pct"`   // R14
    SharedFiles   int       `json:"shared_files"`
    SharedPct     float64   `json:"shared_pct"`  // R14
    Rows          []ReadRow `json:"rows,omitempty"`
}

func SummarizeReads(evs []ReadEvent) ReadsSummary // Available = len(evs) > 0 (R19)
```

Algoritmo de R14: se hace `sort.SliceStable` por `TS`. Para cada lectura se busca en `seen[path]` (el conjunto de agentes que ya leyeron esa ruta). Si ese conjunto tiene algún agente distinto del actual, la lectura suma una relectura entre agentes. Después se agrega el agente actual al conjunto. Ejemplo: A, A, B → 1 relectura entre agentes. A, B, A → 2.

### Acción: `internal/guard/guard.go`

`Action` gana `Partial bool \`json:"partial,omitempty"\``. Solo lo llenan los parsers para Read; `Evaluate` no lo usa. El comentario de `guard.Read` cambia a "la registran todas las lecturas (guard --reads); Evaluate la permite".

### Parsers

- `internal/adapters/agent/claude/hook.go`: `hookInput.ToolInput` gana `Offset, Limit json.RawMessage` (`json:"offset"`, `json:"limit"`). En `case "Read"`, `Partial = present(Offset) || present(Limit)`, donde `present` es una función privada que es verdadera si el valor no está vacío y no es `null`.
- `internal/adapters/agent/opencode/hook.go`: `args` gana `Offset, Limit json.RawMessage`. En `case "read"` se aplica la misma regla.

### Guard: `internal/cli/hookcmds.go`

- `onlyForeignReads` se elimina. En su lugar entra `allReads(acts []guard.Action) bool`, que es verdadera si hay acciones y todas son Read. Cuando `allReads` es verdadera, `runGuard` carga la config (si falla, permite) y llama a `recordReads` sin pasar por el bucle de `Evaluate` (R9).
- `recordReads(c, cfg, acts, cwd, readHook bool)` mantiene la firma:
  1. Si `c.Build` es nil o no hay ninguna acción, sale.
  2. Arma el engine, llama a `Active` y luego a `e.Store.Load(id)` para obtener la fase. Si algo falla, sale (R5, R6).
  3. Para cada acción Read con una ruta que `Normalize` acepta, crea un `metrics.ReadEvent{TS: now, Agent: readAgent(a), Phase: string(rec.Flow.Phase), Path: n, Partial: a.Partial, Tool: readTool(c)}`. Todas las líneas van en un solo `AppendFile(id, metrics.ReadsAllFile, ...)` (R1, R4).
  4. Si la fase es `Quality`, las acciones `isReviewer` se escriben en `review.ReadsFile` con `review.FromAction`, igual que hoy (R10). Ya no se llama a `Status`; la fase sale del mismo `Load`.
- `readAgent(a guard.Action) string`: si `Agent` no está vacío, devuelve `TrimPrefix(a.Agent, flow.SubagentPrefix)`. Si está vacío y `Subagent` es true, devuelve `metrics.UnknownAgent`. Si no, `metrics.MainSession`.
- `readTool(c *Ctx) string`: si el flag `tool` viene vacío, devuelve `"claude"`; si no, su valor.
- El texto del flag `--reads` cambia a "el hook también ve Read: registra las lecturas de todos los agentes (y las del reviewer en quality para la cobertura)".

### Plugin de OpenCode: `adapters/opencode/plugin.js` (embebido, lo escribe `bflow render`)

- `run(args, input, wait = true)`: cuando `wait` es false, se lanza con `stderr: "ignore"` y se devuelve sin esperar.
- En `tool.execute.before`, si `input.tool === "read"` y no es la subsesión `bflow-reviewer`, llama a `run(["guard","--tool","opencode","--reads"], {...mismo input...}, false)` y sale (R12). Errores de `spawn` se atrapan con el `catch` que ya existe. Nunca se lanza una excepción ni se avisa.
- La entrada que recibe el guard no cambia: `agent` es `""` en la sesión raíz y el nombre del agente en una subsesión.

### Stats: `internal/cli/metricscmds.go` y `internal/cli/reads.go` (nuevo)

- `stats` gana el flag `fs.Bool("reads", false, "lecturas por archivo y relectura entre agentes (requiere ID)")`.
- `readReadEvents(e *engine.Engine, id string) metrics.ReadsSummary` en `reads.go`: si no puede leer el archivo, devuelve un `ReadsSummary{}` (con `Available` false).
- `renderReads(s metrics.ReadsSummary) string`. Sin datos, imprime `  lecturas: sin registro de lecturas`. Con datos:
  - `lecturas: 42 · archivos 17 · parciales 9`
  - `relectura entre agentes: 11 de 42 (26.2%) · archivos compartidos: 5 de 17 (29.4%)`
  - tabla `ruta | lecturas | parciales | agentes | quiénes`, con `quiénes` separado por comas y como máximo 30 filas (R16).
- `runStats` con ID arma `data := map[string]any{"stats": st}` y `text := statsLine(st)`. Con `--calls` agrega `data["calls"]` y su bloque, igual que hoy. Con `--reads` agrega `data["reads"]` y el bloque `renderReads`. Sin flags, el texto sigue siendo `renderStats(st)` (R18, R21). Hay que conservar exactamente la salida actual de `--calls` solo, incluido el caso `!st.TokensAvailable`.

### Seguridad y privacidad

- No se guardan rutas absolutas ni el home (R4). La ruta pasa por `Normalize` antes de escribirse.
- No se guarda el contenido leído, solo la ruta.
- El archivo vive en `.bflow/tasks/<ID>/`, que ya tiene la protección de los demás archivos de la tarea. Un agente no puede escribir ahí sin pasar por el guard de Edit/Write.
- Lo que manda el plugin pasa por `ParseActions`, que valida la forma de la entrada. Si es inválida, permite sin registrar.

### Descartado

- Ampliar `reads.jsonl`: pone en riesgo `review_incomplete`.
- Registrar Grep y Glob: no son lecturas de un archivo concreto y harían más ruido.
- Estimar tokens por archivo: `calls.jsonl` cuenta tokens por mensaje, no por resultado de herramienta.
- Hooks async de Claude Code: dependen de la versión y no hacen falta para medir. Se reconsideran si la latencia resulta un problema.

## Tasks

- [x] **T1 Contrato.** Tipos y firmas exactas de Design (`metrics.ReadEvent`, `ReadRow`, `ReadsSummary`, `ReadsAllFile`, `UnknownAgent`, `ParseReadEvents`, `SummarizeReads`, `guard.Action.Partial`, `readAgent`, `readTool`, `allReads`, `renderReads`, `readReadEvents`) con cuerpos vacíos o `panic("TODO")`, que compilan. Las pruebas, por ahora en rojo, son:
  - `internal/metrics/reads_test.go`: `TestParseReadEventsSkipsInvalid` (R20), `TestSummarizeReadsCrossRereads` (R14: A,A,B → 1; A,B,A → 2; empate de ts conserva el orden), `TestSummarizeReadsOrder` (R15), `TestSummarizeReadsEmpty` (R19).
  - `internal/adapters/agent/claude/hook_test.go`: `TestParseReadPartial` (R7: offset, limit, null, ausente).
  - `internal/adapters/agent/opencode/hook_test.go`: `TestParseActionsReadPartial` (R7).
  - `internal/cli/review_test.go`: `TestGuardRecordsAllReads` (R1, R2, R3, R5, R8: main, implementer, reviewer, un agente que no es de bflow, subagente sin nombre; fase del store), `TestGuardReadsAllOutsideRepo` (R4), `TestGuardReadsAllNoActiveTask` (R6), `TestGuardReadsAllSilent` (R9: exit 0 sin salida aunque falle la escritura), `TestGuardReviewerReadsUnchanged` (R10: `reads.jsonl` igual que antes y la misma lectura en los dos archivos). `TestGuardIgnoresOtherReads` se queda como está porque ya comprueba que `reads.jsonl` no cambia.
  - `internal/cli/opencode_hooks_test.go`: `TestOpenCodeGuardRecordsReads` (R1, R8 con `tool: opencode`).
  - `internal/adapters/agent/opencode/plugin_test.go`: `TestPluginReadsOnlyReviewer` se reescribe como `TestPluginReadsFireAndForget` (R12: el read que no es del reviewer llama a `run` con `false` antes de esperar; el del reviewer espera).
  - `internal/cli/reads_test.go`: `TestStatsReadsText` (R13, R16), `TestStatsReadsJSON` (R17), `TestStatsReadsLegacy` (R19), `TestStatsReadsWithCalls` (R21), `TestStatsReadsNeedsID` (R22). `TestStatsJSONUnchangedWithoutCalls` sigue en verde (R18).
- [x] **T2 Agregación.** Implementar `ParseReadEvents` y `SummarizeReads` en `internal/metrics/reads.go`. Las pruebas de metrics pasan a verde. (R14, R15, R19, R20)
- [x] **T3 Parsers con `partial`.** Agregar `guard.Action.Partial` y `offset/limit` en los parsers de Claude y OpenCode. (R7)
- [x] **T4 Registro en el guard.** Cambiar `onlyForeignReads` por `allReads`, escribir el nuevo `recordReads` con `Store.Load` para la fase, y agregar `readAgent` y `readTool`. `reads.jsonl` no cambia. (R1 a R6, R8 a R11)
- [x] **T5 Plugin de OpenCode.** `run(..., wait)` y el read sin esperar para los agentes que no son el reviewer. Reescribir la prueba del plugin. (R12)
- [x] **T6 `stats --reads`.** Flag, `readReadEvents`, `renderReads`, `runStats` que arma la salida por bloques y la combinación con `--calls`. (R13, R16 a R19, R21, R22)
- [x] **T7 Verificación.** `go test ./...` y `go vet` en verde. Correr `bflow render` en este repo para que `.opencode/plugins/bflow.js` quede al día si existe. Medir a mano la latencia de un Read de la sesión principal con el hook, antes y después del cambio, y anotarla en el PR.
