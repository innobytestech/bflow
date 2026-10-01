# GH-29 · Tokens por llamada: tabla de releído y contexto en la UI y en stats

## Brief

**Objetivo:** guardar una fila por llamada del modelo y mostrarla, por corrida de agente, con el acumulado de lo releído y de lo nuevo, en `bflow stats <ID> --calls` y en el panel web.

**Entra**
- `.bflow/tasks/<ID>/calls.jsonl`: una línea por muestra (corrida, agente, fase, modelo, hora, id de mensaje, input, cache write, cache read, output), escrita por el hook de tokens junto a las entradas `tokens` de log.jsonl. Aplica a Claude Code y a OpenCode.
- `bflow stats <ID> --calls`: una tabla por corrida con columnas #, hora, modelo, entrada, caché escrita, releído, salida, contexto, acum. releído y acum. nuevo, más una fila de totales. Con `--json`, `data.calls`.
- Panel web (`watch --web` / `ui`): un bloque por corrida con resumen siempre visible; la tabla se abre con clic y la corrida más reciente sale abierta.
- Aviso "sin detalle por llamada (registrado antes de GH-29)" en tareas viejas.

**No entra**
- Un decimal y contexto final en los resúmenes (GH-11); `metrics.Human` no cambia.
- Reconstruir llamadas de tareas anteriores desde transcripts.
- El panel de terminal (`watch` sin `--web`) y `stats` sin `--calls`, que quedan igual, también en `--json`.

**Decisiones nuevas**
- [N] Cada pasada del hook escribe sus deltas con el id de mensaje, y al leer se fusionan las filas con la misma corrida e id. *Descartado:* escribir solo llamadas completas, porque el hook no sabe si una respuesta ya terminó.
- [N] La corrida se identifica como `<herramienta>:<nombre del transcript sin extensión>` (en Claude, `claude:agent-a1b2`; en OpenCode, `opencode:<sessionID>`). *Descartado:* guardar la ruta completa, que filtra el home del usuario y no aporta nada.
- [N] El contexto (input + cache read + cache write) se calcula al leer, no se guarda. Así las filas fusionadas no lo duplican.
- [N] Las tablas del panel se arman en Go con texto ya formateado; el JS solo las pinta. Así CLI y web comparten el formato de cifras (`metrics.Exact`: 17,711).
- [N] Etiqueta de corrida: el nombre del agente, con `#n` (por orden de primera llamada) solo si ese agente tiene dos o más corridas en la tarea; la sesión principal se llama "sesión principal".
- [N] Si el agregado de log.jsonl es mayor que la suma de las corridas (la tarea empezó antes de GH-29), el panel y `--calls` lo dicen: "parte del gasto se registró antes de GH-29".

**Riesgos**
- Las filas de un subagente aparecen cuando termina (el hook corre en SubagentStop), no mientras trabaja. "La corrida en curso abierta" queda en "la corrida con la llamada más reciente".
- Tamaño de la respuesta del panel: una tarea larga puede tener cientos de filas en cada poll. Es texto corto y el JS solo repinta si cambió (`changed`).
- Si el append a calls.jsonl falla y el de log.jsonl no, los totales dejan de cuadrar. Ambos se escriben bajo el mismo lock y el hook no falla; una prueba cubre el caso normal.

**Tamaño:** M. Toca metrics (tipos nuevos, `Phases`, lectura y agrupación), los dos lectores de transcript (id de mensaje), el hook de tokens, `stats`, el panel (Go + index.html), las pruebas, la guía, el README y el changelog.

## Discovery

bflow ya lee el uso de cada llamada (`metrics.Sample`, en claude/transcript.go y opencode/transcript.go), pero `addTokens` solo guarda en log.jsonl el agregado por fase y modelo. Decidido en el discovery: archivo `calls.jsonl` por tarea; una tabla por corrida con un acumulado que reinicia en cada una; aviso para las tareas históricas; bloque plegable en la UI; cifras exactas; `--json` sin cambios salvo con `--calls`. Las filas deben sumar lo mismo que los eventos `tokens`.

## Requirements

- **R1** [D] CUANDO el hook de tokens registra muestras de una corrida para la tarea activa, el sistema DEBE agregar a `.bflow/tasks/<ID>/calls.jsonl` una línea por muestra con ts, run, tool, agent, phase, model, msg, input, cache_write, cache_read y output.
- **R2** [D] El sistema DEBE asignar a cada fila la fase con la misma regla que `Allot`: en un agente, la fase de su último reporte (sin reporte, la fase en que empezó); en la sesión principal, la fase en que ocurrió la llamada.
- **R3** [D] CUANDO se lee calls.jsonl, el sistema DEBE fusionar en una sola fila las líneas con el mismo `run` y `msg`, sumando sus tokens y conservando la hora, la fase y el modelo de la primera.
- **R4** [D] El sistema DEBE agrupar las filas por `run` y ordenar cada corrida por hora. El número de fila y los acumulados (releído = suma de cache_read; nuevo = suma de input + cache_write + output) DEBEN reiniciar en cada corrida.
- **R5** [D] La suma de tokens de todas las corridas de una tarea DEBE ser igual a la suma de los eventos `tokens` de log.jsonl que se registraron después de GH-29 para esa tarea.
- **R6** [D] CUANDO calls.jsonl tiene líneas que no son JSON válido, o les falta `run` o `msg`, el sistema DEBE ignorarlas y mostrar el resto.
- **R7** [D] CUANDO se ejecuta `bflow stats <ID> --calls`, el sistema DEBE imprimir la línea de resumen de la tarea y, por cada corrida, un encabezado (etiqueta, fase, llamadas, contexto final, total releído, total nuevo) y la tabla de columnas #, hora, modelo, entrada, caché escrita, releído, salida, contexto, acum. releído y acum. nuevo, con una fila final de totales. Las cifras van exactas, con separador de miles.
- **R8** [D] CUANDO la tarea tiene tokens registrados pero no tiene calls.jsonl (o no tiene filas válidas), `stats --calls` y el panel DEBEN mostrar "sin detalle por llamada (registrado antes de GH-29)". Sin tokens registrados, `--calls` DEBE mostrar el mismo texto de "no disponibles" que `stats`.
- **R9** [N] CUANDO el agregado de log.jsonl supera la suma de las corridas en tokens nuevos o en releídos, `stats --calls` y el panel DEBEN agregar "parte del gasto se registró antes de GH-29".
- **R10** [D] CUANDO se ejecuta `bflow stats <ID> --calls --json`, el sistema DEBE devolver `data.stats` como hoy más `data.calls` (lista de corridas con sus filas). Sin `--calls`, la salida JSON de `stats` DEBE quedar idéntica a la de hoy.
- **R11** [N] CUANDO se ejecuta `bflow stats --calls` sin ID, el sistema DEBE fallar con código `usage` y el mensaje "--calls requiere un ID".
- **R12** [D] CUANDO el panel web muestra una tarea con filas, DEBE mostrar un bloque por corrida con un resumen siempre visible (etiqueta, llamadas, contexto final, total releído, total nuevo) y la tabla de R7 plegable con clic. La corrida con la llamada más reciente DEBE aparecer abierta.
- **R13** [N] CUANDO el panel se refresca, el sistema DEBE conservar abiertas o cerradas las corridas que la persona abrió o cerró, identificadas por su `run`.
- **R14** [D] El hook DEBE escribir calls.jsonl bajo el lock del cursor que ya usa `tokensFrom`, y nunca DEBE fallar ni imprimir aunque no pueda escribir.
- **R15** [D] La UI DEBE usar texto en color, sin emojis, y los datos de calls.jsonl DEBEN pintarse como texto (textContent), nunca como HTML.

## Design

### Datos

`metrics.Sample` gana el id de mensaje:

```go
type Sample struct {
	TS    time.Time
	Model string
	Msg   string // id de mensaje: claude e.Message.ID; opencode "opencode:"+l.ID (la misma llave de cur.Seen)
	Usage
}
```

Línea de calls.jsonl (nuevo archivo `internal/metrics/calls.go`):

```go
// Call es una muestra de una llamada del modelo tal como se guarda: puede ser
// un delta de una respuesta que otra pasada del hook ya había leído en parte.
type Call struct {
	TS         time.Time  `json:"ts"`
	Run        string     `json:"run"`   // RunKey
	Tool       string     `json:"tool"`  // claude | opencode
	Agent      string     `json:"agent"` // MainSession o nombre sin prefijo
	Phase      flow.Phase `json:"phase"`
	Model      string     `json:"model,omitempty"`
	Msg        string     `json:"msg"`
	Input      int64      `json:"input"`
	CacheWrite int64      `json:"cache_write"`
	CacheRead  int64      `json:"cache_read"`
	Output     int64      `json:"output"`
}

func (c Call) Context() int64 // Input + CacheRead + CacheWrite
func (c Call) New() int64     // Input + CacheWrite + Output (igual que Usage.New)

// CallRow es una llamada ya fusionada, con su lugar en la corrida.
type CallRow struct {
	N       int   `json:"n"`
	Call
	Context int64 `json:"context"`
	AccRead int64 `json:"acc_read"`
	AccNew  int64 `json:"acc_new"`
}

// Run es una corrida: un transcript de un agente o de la sesión principal.
type Run struct {
	Run          string    `json:"run"`
	Agent        string    `json:"agent"`
	Label        string    `json:"label"` // "implementer", "implementer #2", "sesión principal"
	Phase        flow.Phase `json:"phase"` // de la primera fila
	Rows         []CallRow `json:"rows"`
	Total        Usage     `json:"total"` // Calls = len(Rows), MaxContext = máximo
	FinalContext int64     `json:"final_context"`
	Last         time.Time `json:"last"`
}

const CallsFile = "calls.jsonl"

func RunKey(tool, path string) string            // tool + ":" + base sin extensión
func ParseCalls(r io.Reader) []Call               // salta líneas malas (R6)
func Runs(calls []Call) []Run                     // fusiona (R3), agrupa y acumula (R4); orden por primera llamada
func Exact(n int64) string                        // 17711 -> "17,711"; 0 -> "0"
func Phases(entries []store.Entry, id, agent string, samples []Sample) []flow.Phase // una por muestra (R2)
```

`Allot` se reescribe sobre `Phases` sin cambiar su resultado (TestAllot sigue pasando). La etiqueta `#n` sale en `Runs`: se cuentan las corridas por agente y se numeran por orden de la primera llamada, solo si hay dos o más. La sesión principal es "sesión principal" (con `#n` si hay varias) y un agente "?" se muestra como "?".

### Escritura (hook)

- `tokensFrom`: cada `batch` guarda también `run: metrics.RunKey(tool, src.Path)`. `addTokens(e, id, tool, run, samples, agent)` calcula `Phases`, arma una `Call` por muestra y llama a `e.Store.AppendFile(id, metrics.CallsFile, lines)` justo después de `Store.Append` de las entradas `tokens`, todavía bajo el lock del cursor (R14). Ignora el error.
- `store.Store.AppendFile(id, rel string, lines [][]byte) error`: resuelve la ruta con `Path` (valida id y que no salga de la carpeta), crea el directorio, abre con O_APPEND y escribe todo en un solo `Write`, con un `\n` por línea. No toma un lock propio: el único escritor es el hook, que ya tiene el del cursor.
- Las muestras que el lector descarta hoy (`delta.Total() == 0`) siguen sin escribirse.

### Lectura

- `cli.readCalls(e, id) ([]metrics.Run, bool)`: `Store.ReadFile(id, CallsFile)`; si el archivo no existe, devuelve `nil, false`; si existe, `Runs(ParseCalls(...))`.
- `callsNote(st metrics.TaskStats, runs []metrics.Run) string`: devuelve "" si no hay tokens (R8: el texto de "no disponibles" lo pone el render); "sin detalle por llamada (registrado antes de GH-29)" si `runs` está vacío; "parte del gasto se registró antes de GH-29" si `st.Tokens.New()` o `st.Tokens.CacheRead` es mayor que la suma de las corridas (R9).

### CLI

- `stats` gana `Setup` con `fs.Bool("calls", false, "una tabla por llamada del modelo, por corrida (requiere ID)")`. Sin ID y con `--calls`: `output.Fail("usage", ...)` (R11).
- Con `--calls`: `env.Data = {"stats": st, "calls": runs}` y `env.Text = statsLine(st) + "\n" + renderCalls(runs, note)`. Sin `--calls`, el código actual no cambia (R10).
- `renderCalls`: por corrida, el encabezado `implementer #2 · implementing · 9 llamadas · contexto final 60,079 · releído 344,086 · nuevo 61,204`, las filas alineadas a la derecha con `Exact`, la hora en local `15:04:05` y una fila `total` con las sumas (contexto = máximo).

### Panel web

- `watchData` gana `Calls []metrics.Run` y `CallsFile bool`, que se llenan en `readWatchIn` solo para la tarea activa.
- `panelTask` gana `Runs []panelRun \`json:"runs,omitempty"\`` y `CallsNote string \`json:"calls_note,omitempty"\``.

```go
type panelRun struct {
	Key     string     `json:"key"`     // Run.Run: el JS recuerda abierto/cerrado con él
	Label   string     `json:"label"`
	Summary string     `json:"summary"` // "9 llamadas · contexto final 60,079 · releído 344,086 · nuevo 61,204"
	Open    bool       `json:"open"`    // la corrida con la llamada más reciente
	Rows    [][]string `json:"rows"`    // celdas ya formateadas, mismo orden de columnas que R7
	Total   []string   `json:"total"`
}
```

- `buildPanel` los arma con las mismas funciones de formato que `renderCalls` (una sola función `callCells(CallRow) []string`, que comparten CLI y web).
- index.html: `renderGauges` agrega `runs` y `calls_note` a `changed`; bajo la sección Tokens, un `<details>` por corrida (el `<summary>` lleva la etiqueta y el resumen) con una `<table>` dentro. Un `Map` `runOpen` guarda la elección de la persona por `key` (evento `toggle`); sin elección, usa `open` del servidor (R13). Las celdas se crean con `el()` (textContent). Se usan las variables de color que ya existen, sin emojis (R15).

### Descartado

- Guardar las llamadas en log.jsonl: lo haría crecer y lo leen `stats`, `watch` y el hook en cada turno.
- Calcular la tabla en JS a partir de números crudos: duplicaría el formato.
- Poner las llamadas en `stats --json` sin flag: va contra la economía de tokens de los agentes.

### Seguridad

- Authz: el panel solo sirve repos registrados con Host permitido (sin cambio). `--calls` solo lee la carpeta de la tarea pedida, y `Store.Path` valida el id y bloquea el path traversal.
- Datos sensibles: calls.jsonl guarda solo números, nombres de modelo y agente, ids opacos de mensaje y de corrida. No guarda rutas ni contenido de mensajes, y vive en `.bflow/`, que ya está ignorado.
- Validación: las líneas corruptas se ignoran (R6). En el JSON de un `Call`, un número negativo o ausente queda tal cual o en 0, y no tumba la lectura.
- Errores: el hook nunca falla (R14). `stats --calls` sin archivo muestra un aviso, no un error.

## Tasks

- [ ] **T1 Contrato** (R1-R15): tipos y firmas sin lógica (`Sample.Msg`, `Call`, `CallRow`, `Run`, `CallsFile`, `RunKey`, `ParseCalls`, `Runs`, `Exact`, `Phases`, `Store.AppendFile`, `panelRun`, campos nuevos de `panelTask` y `watchData`) y estas pruebas, que deben fallar:
  - metrics: `TestRunKey`, `TestExact`, `TestParseCallsSkipsBadLines`, `TestRunsMergesSameMessage`, `TestRunsAccumulatesPerRun`, `TestRunsLabels`, `TestPhasesMatchesAllot`
  - adaptadores: `TestReadUsageSetsMsg` (claude y opencode)
  - store: `TestAppendFileStaysInTaskDir`
  - cli: `TestHookTokensWritesCalls`, `TestCallsMatchTokenEvents`, `TestStatsCallsText`, `TestStatsCallsJSON`, `TestStatsJSONUnchangedWithoutCalls`, `TestStatsCallsNeedsID`, `TestStatsCallsLegacyNote`, `TestStatsCallsPartialNote`, `TestPanelRuns`
- [ ] **T2 Lectores** (R1): `Msg` en `Sample` en claude/transcript.go y opencode/transcript.go. Pasa `TestReadUsageSetsMsg`.
- [ ] **T3 metrics** (R2-R6): `Phases` (con `Allot` reescrito sobre ella), `RunKey`, `ParseCalls`, `Runs`, `Exact`. Pasan las pruebas de metrics y TestAllot.
- [ ] **T4 Escritura** (R1, R2, R5, R14): `Store.AppendFile` y el hook (`tokensFrom` → `addTokens` con `run`). Pasan `TestAppendFileStaysInTaskDir`, `TestHookTokensWritesCalls` (Claude y OpenCode; un mensaje leído en dos pasadas deja una fila) y `TestCallsMatchTokenEvents`.
- [ ] **T5 CLI** (R7-R11): flag `--calls`, `readCalls`, `callsNote`, `callCells` y `renderCalls`. Pasan las pruebas `TestStatsCalls*` y `TestStatsJSONUnchangedWithoutCalls`.
- [ ] **T6 Panel web** (R8, R9, R12, R13, R15): `readWatchIn`, `buildPanel` e index.html (`<details>` por corrida, `runOpen`). Pasa `TestPanelRuns` (`open` en la más reciente, filas formateadas, `calls_note`).
- [ ] **T7 Docs** (R7, R12): `docs/guia.md` (sección de stats: `--calls` y cómo leer el acumulado: lo releído en la llamada N es el contexto de la N-1), la fila de Métricas del `README.md` (`stats [ID] [--calls]`) y `CHANGELOG.md`.
