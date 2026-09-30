# GH-6 · Intake en status y bflow new

## Brief

**Objetivo:** que una idea sin tarea entre al flujo desde `bflow status`: crear la tarea en el tracker y arrancarla en un carril con un solo comando.

**Entra**
- `status` sin tareas en curso (cero vistas abiertas) devuelve `next` = `ask`, gate `intake`, skill `intake`, con dos opciones: «Crear tarea nueva» (`bflow new --lane <carril> --title "<título>" --file <idea.md>`) y «Usar una existente» (`bflow start <ID> --lane <carril>`).
- `bflow new --lane <carril> --title <título> [--file <idea.md>]`: `CreateTask` + `Start` en un comando; el contenido del archivo va como descripción.
- `Create` en el adaptador de Plane (`POST .../work-items/`).
- `bflow task list`: tareas abiertas del tracker (ID y título), para que la skill busque títulos parecidos antes de crear; hoy ningún comando expone `List`.
- Sección `## intake` en la skill (`adapters/claude/skills/bflow/SKILL.md`).
- README (tabla de comandos) y CHANGELOG.

**No entra**
- El agente `bflow-scout` (parte B de #4).
- Con varias tareas en curso, `status` sigue devolviendo la lista (no hay intake: hay que elegir una).
- Mudar tareas entre trackers.

**Decisiones nuevas**
- [N] Sin respaldo en local cuando el tracker no implementa `Creator`: los tres adaptadores (local, github, plane) lo implementan tras esta tarea, y una LOCAL-n con tracker Plane no se podría leer con `Get`. Se mantiene el rechazo actual de `CreateTask`. Descartado: tracker compuesto que enrute por prefijo.
- [N] `new` valida el carril contra `flow.lanes` antes de crear nada, para no dejar tareas huérfanas en el tracker por un carril mal escrito. Descartado: crear y dejar que `Start` rechace.
- [N] Si `Start` falla después de crear, `new` no borra la tarea: responde el error con el ID creado y el comando `bflow start <ID> --lane <carril>` para reintentar. Descartado: borrar la tarea (no hay `Delete` en la interfaz y el tracker puede tener ya notificaciones).
- [N] `--file` es opcional (sin él, descripción vacía); la skill siempre lo pasa. Descartado: obligatorio, que impide `new` rápido desde la terminal.
- [N] `task list` como comando aparte; descartado: meter la lista del tracker en el `next` de intake, que haría que cada `status` sin tarea llame al tracker.
- [N] `intake` no es un `flow.Gate` de la máquina (como `blocked`): no se aprueba ni se rechaza, solo guía. Descartado: agregarlo a `types.go`, que lo haría aprobable con `bflow approve`.

**Riesgos:** los hooks que llaman `status --brief` sin tarea ahora reciben un `next` con opciones; el texto `--brief` no cambia. El JSON de `status_list` gana `next` (antes `done`).

**Tamaño:** light. Unos 6 archivos de producción (`flow/next.go`, `cli/flowcmds.go`, `engine/commands.go`, `plane/plane.go`, skill, README/CHANGELOG) y sus pruebas.

## Discovery

<!-- Carril light: sin discovery. -->

## Requirements

<!-- Carril light: los criterios van en Tasks. -->

## Design

<!-- Carril light: los nombres exactos van en Tasks. -->

## Tasks

- [x] **T1 · Next de intake** (`internal/flow/next.go`)
  - Nueva `func IntakeNext(cfg Config) output.Next` y constante no exportada a la máquina `IntakeGate = "intake"` (string, fuera de `Gate`).
  - R1: CUANDO se llama `IntakeNext`, DEBE devolver `Action: ask`, `Gate: "intake"`, `Skill: "intake"`, `Question` «¿Creamos una tarea nueva para la idea o usamos una que ya existe?» y dos opciones: `{ID: "new", Label: "Crear tarea nueva", Command: "bflow new --lane <carril> --title \"<título>\" --file <idea.md>"}` y `{ID: "existing", Label: "Usar una existente", Command: "bflow start <ID> --lane <carril>"}`. La descripción de cada opción lista los carriles de `cfg.Lanes` (mismo orden que `laneOptions`).
  - Prueba: `TestIntakeNext` en `internal/flow/next_test.go`.

- [x] **T2 · status sin tareas en curso** (`internal/cli/flowcmds.go`, `runStatus`)
  - R2: CUANDO `status` corre sin ID y `e.Views` devuelve cero tareas, DEBE responder `kind` `status_list` con `next` = `flow.IntakeNext(e.Cfg.Flow.Core())` (lo mismo que `Engine.flowCfg()`); el texto sin `--brief` agrega `renderNext(next)` tras «bflow: sin tareas en curso»; con `--brief`, el texto no cambia.
  - R3: CUANDO hay una o varias tareas en curso, el comportamiento actual no cambia.
  - Prueba: `TestStatusWithoutTasksAsksIntake` en `internal/cli`.

- [x] **T3 · Create en Plane** (`internal/adapters/tracker/plane/plane.go`)
  - `func (c *Client) Create(ctx context.Context, title, description string) (tracker.Task, error)` y `_ tracker.Creator = (*Client)(nil)`.
  - R4: DEBE hacer `POST <proj>/work-items/` con `{"name": title, "description_html": markdown.ToHTML(description)}`, llamar `c.remember(w)` y devolver `c.task(sts, w)` (ID `PROY-<sequence_id>`, estado por defecto del proyecto).
  - R5: CUANDO la API responde error, DEBE devolverlo sin tarea parcial.
  - Pruebas: `TestCreate` y `TestCreateError` en `plane_test.go` (el fake gana `POST proj+"/work-items/"`).

- [x] **T4 · bflow new** (`internal/cli/flowcmds.go`, `internal/engine/commands.go`)
  - `func (e *Engine) New(ctx context.Context, lane flow.Lane, title, desc string) (tracker.Task, Outcome, error)`; comando `Register(&Command{Name: "new", ...})` con flags `--lane`, `--title`, `--file`, `--slug`.
  - R6: CUANDO falta `--lane` o `--title` (tras `TrimSpace`), DEBE fallar con `usage` sin llamar al tracker.
  - R7: CUANDO el carril no está en la config del flujo, DEBE rechazar (exit 2, código `unknown_lane`) sin crear la tarea.
  - R8: CUANDO `--file` no se puede leer (ruta relativa a `c.Dir`, vía `abs`), DEBE fallar sin crear la tarea.
  - R9: CUANDO el tracker no implementa `Creator`, DEBE devolver el error actual de `CreateTask`.
  - R10: CUANDO crea bien, DEBE correr `Start(ctx, id, lane, slug, "")` y responder como `start` (mismo `next`, `data.id`, `data.title`), incluida la apertura del panel si `ui.watch`/`ui.web`.
  - R11: CUANDO `Start` falla tras crear, DEBE responder el error con `data.id` y el texto «<ID> creada, pero no arrancó: <motivo>. Reintenta con bflow start <ID> --lane <carril>».
  - Pruebas: `TestNewCreatesAndStarts`, `TestNewRejectsUnknownLaneBeforeCreate`, `TestNewStartFailureKeepsTask` en `internal/engine` con `trackertest.Memory`.

- [x] **T5 · bflow task list** (`internal/cli/flowcmds.go`, `internal/engine/commands.go`)
  - `func (e *Engine) OpenTasks(ctx context.Context) ([]tracker.Task, error)` = `e.Tracker.List(ctx, tracker.Filter{OpenOnly: true})`; comando `Register(&Command{Name: "task list", ...})`.
  - R12: DEBE responder `kind` `task_list` con `data.tasks` = `[{id, title, phase}]` (solo esos campos, JSON compacto) y texto de una línea por tarea `ID · fase · título`.
  - R13: CUANDO el tracker falla, DEBE responder el error (`fail`).
  - Prueba: `TestTaskListOnlyOpen` en `internal/cli` o `internal/engine`.

- [x] **T6 · Skill, README y CHANGELOG**
  - R14: `SKILL.md` gana `## intake` (≤5 líneas): pedir la idea si no la hay; correr `bflow task list --json` y, si un título se parece, ofrecer usar esa tarea antes de crear; proponer título y carril con su motivo; guardar la idea en un archivo y correr el `command` elegido con los marcadores reemplazados.
  - README: `new` y `task list` en la tabla de comandos (línea de «Flujo») y un ejemplo junto a `task add`. CHANGELOG: entrada en la versión en curso.
