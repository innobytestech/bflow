# GH-8 · El split deja la tarea madre bloqueada para siempre

## Brief

**Objetivo:** que aprobar un split cree las tareas hijas y retire la madre, y que cualquier tarea empezada se pueda retirar con `bflow drop`.

**Entra**
- Fase terminal nueva `dropped`. `status`, `panel` y el statusline la tratan como cerrada. El tracker la cierra como no planeada: en GitHub con `state_reason: not_planned`, en Plane con un estado del grupo `cancelled` y en local como cerrada.
- `### División` dentro del Brief, con una viñeta `- **<título>**: <alcance>` por hija. Si falta o está mal escrita, `report SPLIT` sale con código 2.
- Al aprobar el gate `split` se crean las hijas con `Creator` (en backlog y sin carril), se comenta la lista en la madre y la madre pasa a `dropped`. Si se reintenta, solo se crean las que faltan.
- `bflow drop <ID> --note "<motivo>"`: funciona desde cualquier fase empezada salvo done/dropped, incluida blocked. Si la tarea tiene rama o PR, lo avisa y no los toca. Solo lo puede correr una persona (guard).
- Oficio del spec-author y `bflow render`.

**No entra**
- La opción "Dividir la feature" del gate `rounds`: sigue bloqueando.
- Sub-issues nativos de GitHub o relaciones padre/hija en Plane.
- Leer como `dropped` un issue que se cerró a mano como no planeado: `bflow panel` lo sigue cerrando como `done`.
- Retirar tareas que no empezaron (no aparecen en status): `drop` responde `not_started`.

**Decisiones nuevas**
- [N] Engine crea las hijas antes de aplicar el evento y guarda cada una en `Record.Split` en cuanto existe. Flow solo recibe la lista (`Event.Children`) y sigue siendo puro. Descartado: un efecto `FxCreateTask` en el núcleo. Los efectos del tracker no bloquean (quedan pendientes), así que la madre se cerraría sin hijas.
- [N] La división se valida al reportar SPLIT y otra vez al aprobar: de 2 a 6 hijas, títulos únicos de 120 runas como máximo y alcance no vacío. Descartado: validar solo al aprobar, porque el error le llegaría al humano y no al agente que lo puede corregir.
- [N] Las hijas que ya existen se reconocen por título. Descartado: reconocerlas por posición, porque si alguien reordena la lista se duplican.
- [N] `drop` exige el ID explícito. Descartado: resolver la tarea activa como los demás comandos, porque retirar la tarea equivocada no tiene vuelta.
- [N] En GitHub, al pasar a `dropped` primero se cierra el issue y después se mueven la etiqueta o el Status. Descartado: el orden que usa `done` (primero el Status), porque en un project sin la opción nueva el issue se quedaría abierto hasta correr `bflow tracker setup`.
- [N] No hay `undrop`. Para retomar el trabajo se crea otra tarea. Descartado: reabrir, porque haría falta deshacer el cierre en cada tracker.

**Riesgos**
- Los projects y proyectos de Plane que ya existen no tienen estado para `dropped`. El cambio de estado queda pendiente, con un aviso, hasta correr `bflow tracker setup`. En GitHub el issue se cierra de todos modos, y `bflow doctor` avisa del estado que falta.
- Si bflow muere después de crear una hija y antes de guardarla, al reintentar la duplica. La ventana es de un request.

**Tamaño:** grande para el carril full: unos 16 archivos de producción (flow, tracker, adaptadores, engine, store, cli, guard, metrics y el oficio), más sus pruebas. No cabe en light porque cambia la máquina de estados.

## Discovery

Hoy, al aprobar el split la madre pasa a `blocked` y no hay forma de retirarla (GH-4 se limpió a mano). Lo que se decidió: las hijas salen de una sección de división en el Brief y se crean con `Creator` al aprobar; hay una fase terminal `dropped` (GitHub `not_planned`, Plane `cancelled`) que status oculta; `bflow drop <ID> --note` funciona desde cualquier fase salvo done/dropped y no toca git; sin `Creator` se rechaza con código 2; una falla a mitad de camino deja la madre en el gate y el reintento es idempotente; las hijas quedan en backlog sin carril y la madre recibe un comentario con la lista. El gate `rounds` no cambia.

## Requirements

**Fase `dropped`**
- R1 [D] CUANDO llega `drop` a una tarea empezada que no está en done ni en dropped (incluida blocked), flow DEBE pasarla a `dropped`, limpiar el gate y el bloqueo, y pedir los efectos `tracker_state(dropped)` y el comentario `**Retirada:** <motivo>`.
- R2 [D] CUANDO `drop` llega sin motivo (vacío tras `TrimSpace`), flow DEBE rechazar con `note_required` sin cambiar el estado.
- R3 [N] CUANDO la tarea está en `dropped`, flow DEBE rechazar cualquier evento con `task_dropped`. `NextFor` DEBE devolver `action: done` con el motivo "tarea retirada".
- R4 [D] CUANDO la tarea está en `dropped`, `Views` (y con eso `status` sin ID y `Active`) y `Panel` DEBEN omitirla, y el statusline NO DEBE mostrar su duración.
- R5 [D] CUANDO GitHub recibe `Transition(dropped)` y el issue está abierto, DEBE cerrarlo con `state_reason: not_planned` antes de escribir la etiqueta o el Status. Si el issue ya está cerrado, no lo vuelve a cerrar.
- R6 [D] CUANDO Plane recibe `Transition(dropped)`, DEBE escribir en el estado de `dropped` de la tabla (`Cancelled`/`Cancelado`/`Descartado`/`Canceled`, grupo `cancelled`).
- R7 [D] CUANDO el tracker local o `Memory` reciben `Transition(dropped)`, la tarea DEBE quedar `Closed` y `List(OpenOnly)` DEBE omitirla.
- R8 [N] `tracker.PhaseOrder` DEBE incluir `dropped`, para que `EnsureStates` (estados de Plane, etiquetas y opciones de Status de GitHub), `StateMap` y `bflow doctor` lo tomen en cuenta.
- R9 [D] CUANDO la tarea pasa a `dropped`, metrics DEBE dejar de contar tiempo desde ese evento y `TaskStats.Phase` DEBE ser `dropped`. El log DEBE registrar el evento `drop` con la nota.

**`bflow drop`**
- R10 [D] CUANDO se corre `bflow drop` sin ID, DEBE fallar con `usage` sin tocar el estado. CUANDO falta `--note`, DEBE salir con código 2 y `note_required` (R2).
- R11 [D] CUANDO la tarea retirada tiene una rama registrada o un PR sin mergear, la salida DEBE avisarlo en `warnings` y NO DEBE borrar la rama ni cerrar el PR.
- R12 [N] CUANDO un subagente intenta `bflow drop`, guard DEBE negarlo con `human_only`.
- R13 [N] CUANDO la tarea no empezó, `drop` DEBE responder `not_started`, igual que los demás comandos de flujo.

**Split**
- R14 [D] CUANDO el spec-author reporta SPLIT y el Brief no trae una `### División` válida (R15), bflow DEBE rechazar con código 2 y `split_invalid`, indicando qué falta.
- R15 [N] Una división es válida CUANDO tiene entre 2 y 6 hijas, cada una con título no vacío, de 120 runas como máximo y distinto a los demás, y con alcance no vacío.
- R16 [D] CUANDO se muestra el gate `split`, `next.show` DEBE incluir `bflow show <ID> brief` (para que el display traiga la división) y la opción de aprobar DEBE decir "Dividir: crear las hijas y retirar <ID>".
- R17 [D] CUANDO se aprueba el split y el tracker no implementa `Creator`, bflow DEBE rechazar con código 2 y `no_creator` sin crear nada ni cambiar el estado.
- R18 [D] CUANDO se aprueba el split, engine DEBE crear con `Creator` una tarea por cada hija cuyo título no esté en `Record.Split`. El título es el de la hija y la descripción es `<alcance>\n\nParte de <ID madre> · <título madre>`. Cada hija DEBE guardarse en `Record.Split` en cuanto se crea.
- R19 [D] CUANDO falla la creación de una hija, bflow DEBE responder con un error que liste las hijas ya creadas y el comando para reintentar (`bflow approve <ID> --gate split`). La madre DEBE seguir en el gate `split`.
- R20 [D] CUANDO se reintenta la aprobación, bflow DEBE crear solo las hijas que faltan.
- R21 [D] CUANDO ya existen todas las hijas, flow DEBE pasar la madre a `dropped` con un comentario `**Dividida en:**` seguido de una línea `- <ID> · <título>` por hija. Las hijas DEBEN quedar en backlog, sin carril.
- R22 [N] CUANDO llega `approve` del gate `split` sin `Children`, flow DEBE rechazar con `split_children`.
- R23 [N] La entrada del log del approve del split DEBE llevar `data.children` con los IDs de las hijas.
- R24 [D] El oficio del spec-author DEBE pedir que, antes de reportar SPLIT, se escriba la `### División` en el Brief con el formato de R15.

## Design

**flow** (`internal/flow/types.go`, `machine.go`, `next.go`)
- `Dropped Phase = "dropped"`. No entra en `Order`: no la recorre ningún carril.
- `EvDrop EventKind = "drop"`.
- `Event.Children []string \`json:"children,omitempty"\``: en el approve del split, una línea `"<ID> · <título>"` por cada hija creada.
- `tx.apply`: después del chequeo de `Done` va `if s.Phase == Dropped { return reject("task_dropped", "la tarea %s ya se retiró", s.ID) }`. `EvDrop` se atiende antes del chequeo de `Blocked`.
- `func (t *tx) drop(note string) error`: si la nota recortada está vacía, `reject("note_required", "retirar necesita --note con el motivo")`. Si no, `s.Block = nil`, `t.enter(Dropped)` y agrega el comentario `"**Retirada:** "+note`.
- `approve` con `GateSplit`: sin `ev.Children` devuelve `reject("split_children", "aprobar el split necesita las hijas creadas")`. Con hijas, agrega el comentario `"**Dividida en:**\n- " + strings.Join(ev.Children, "\n- ")`, pone `s.Block = nil` y hace `t.enter(Dropped)`. Deja de llamar a `t.block`.
- `NextFor`: `case Dropped` devuelve `output.Next{Action: output.ActionDone, Reason: "tarea retirada"}`.
- `gateNext` con `GateSplit`: `n.Show = []string{cmd("show", id, "brief")}`; la etiqueta de aprobar es `"Dividir: crear las hijas y retirar "+id`.

**tracker** (`internal/tracker/states.go`)
- `DefaultStates[flow.Dropped] = {[]string{"Cancelled", "Cancelado", "Descartado", "Canceled"}, "cancelled", "#6B7280"}`.
- `PhaseOrder` = backlog + `flow.Order` + blocked + dropped.

**Adaptadores**
- GitHub `issues.go` `Transition`: si `to == flow.Dropped` y `is.State != "closed"`, primero `PATCH {"state":"closed","state_reason":"not_planned"}` y después la etiqueta o el Status. El camino de `Done` no cambia. En `setup.go`, `optionColors[flow.Dropped] = "GRAY"`.
- Plane: no cambia el código. `Closed` ya sale del grupo `cancelled`, y `EnsureStates` crea `Cancelled` porque recorre `PhaseOrder`.
- local `local.go` y `trackertest/memory.go`: `Closed` y el filtro `OpenOnly` cuentan `Done || Dropped`. `Memory` gana `FailCreateAfter int`: si es mayor que 0, `Create` devuelve `ErrInjected` después de crear esa cantidad de tareas.
- `trackertest.go` (conformidad): subprueba `"dropped closes"`. Después de `Transition(dropped)`, `Get` devuelve `Closed` y `List(OpenOnly)` no la incluye. Los fakes de GitHub y Plane se ajustan para pasarla.

**store** (`internal/store/store.go`)
- `type SplitChild struct { Title string \`json:"title"\`; ID string \`json:"id"\` }` y `Record.Split []SplitChild \`json:"split,omitempty"\``.

**engine**
- `split.go`, nuevo:
  - `type SplitPart struct{ Title, Scope string }`.
  - `const maxSplitParts = 6` y `maxSplitTitle = 120`.
  - `func ParseSplit(brief string) ([]SplitPart, error)`: busca en el Brief un encabezado `### División` (también acepta `Division`, sin distinguir mayúsculas) y lee hasta el siguiente encabezado. Cada hija es una viñeta de primer nivel `- **<título>**: <alcance>`. Las líneas siguientes con sangría se suman al alcance, sin la sangría. Cualquier violación de R15, una viñeta sin título en negritas o una sección que falta devuelven `*flow.Rejection{Code: "split_invalid"}` con el motivo y la línea.
  - `func (e *Engine) createChildren(ctx context.Context, id string) ([]string, error)`: hace lo de R17 a R20. Primero revisa `Creator` y la división, luego crea las hijas que faltan y persiste cada una con `e.Store.Update` por separado. Devuelve las líneas `"<ID> · <título>"` en el orden del Brief.
- `engine.go` `apply`: si `ev` es un `report` SPLIT en `spec`, antes de `flow.Apply` lee el Brief (`e.specSection(*rec, "brief")`) y corre `ParseSplit`. Con error, rechaza (R14), igual que `contract_hollow`.
- `engine.go` `entry`: si `len(ev.Children) > 0`, agrega `Data["children"]` con los IDs (R23).
- `commands.go`:
  - `Approve`: si el gate pendiente es `split` y `o.Gate` es `""` o `"split"`, primero llama a `createChildren` y pasa el resultado en `Event.Children`.
  - `func (e *Engine) Drop(ctx context.Context, id, note string) (Outcome, error)`: aplica `EvDrop` y agrega los avisos de R11: "la rama <b> sigue existiendo; bórrala a mano si ya no sirve" y "el PR <url> sigue abierto; ciérralo a mano si ya no sirve".
  - `Views` omite `Done` y `Dropped`.
- `panel.go`: el ciclo de `Panel` salta `Dropped`, igual que `Done`.

**cli, guard, metrics**
- `flowcmds.go`: `Register(&Command{Name: "drop", Summary: "retira una tarea sin terminarla: drop <ID> --note \"motivo\""})` con el flag `--note`. Sin `c.Args[0]` devuelve `output.Fail("usage", …)`; no usa `resolveID` para buscar la tarea activa. Si hay ID, llama a `e.Drop` y responde con `outcomeEnvelope`.
- `metricscmds.go` (statusline): no muestra duración en `Done` ni en `Dropped`. `setupcmds.go` (doctor): recorre `tracker.PhaseOrder` en vez de la lista armada a mano.
- `guard.go`: `HumanOnly` suma `"drop"`.
- `metrics.go` `add`: deja de contar cuando `cur == flow.Dropped`.

**Superficie de seguridad**
- Authz: `drop` y `approve` son `HumanOnly`. Un subagente no puede retirar tareas ni disparar la creación de hijas.
- Validación: los títulos y alcances salen de la spec, que escribe un agente. El límite de 2 a 6 hijas y 120 runas por título evita crear issues en masa. Viajan como JSON al tracker, nunca por shell.
- Errores: las fallas de `Creator` no cambian el estado de flujo (R19). El cambio de estado de la madre sigue la regla de efectos pendientes que ya existe.
- Datos sensibles: ninguno nuevo. `drop` no toca git ni credenciales.

## Tasks

- [x] **T1 · Contrato.** Tipos y firmas de Design con stubs que compilan, y las pruebas que deben fallar contra ellos:
  - flow: `Dropped`, `EvDrop`, `Event.Children`. Pruebas: `TestDropFromAnyStartedPhase`, `TestDropFromBlocked`, `TestDropNeedsNote`, `TestDroppedRejectsEvents`, `TestNextForDropped`, `TestApproveSplitDropsWithChildren`, `TestApproveSplitNeedsChildren`, `TestSplitGateShowsBrief`.
  - tracker: `DefaultStates[Dropped]` y `PhaseOrder`. Pruebas: `TestStatesDropped` y la subprueba de conformidad `"dropped closes"`. GitHub: `TestTransitionDroppedClosesNotPlanned`.
  - store: `SplitChild` y `Record.Split`.
  - engine: `SplitPart`, `ParseSplit`, `(*Engine).Drop`. Pruebas: `TestParseSplit` (tabla de R15), `TestReportSplitNeedsDivision`, `TestApproveSplitCreatesChildren`, `TestApproveSplitWithoutCreator`, `TestApproveSplitResumesAfterFailure`, `TestDropWarnsBranchAndPR`, `TestViewsHideDropped`.
  - cli, guard, metrics: `TestDropRequiresID`, `TestHumanOnlyDrop`, `TestDroppedStopsClock`.
  - `Memory.FailCreateAfter`.
- [x] **T2 · Fase `dropped` en flow** (R1, R2, R3, R21 en la parte de flow, R22). `machine.go` y `next.go`, incluido el gate `split` (R16).
- [x] **T3 · `dropped` en tracker y adaptadores** (R5, R6, R7, R8). `states.go`, GitHub `issues.go`/`setup.go`, local, `Memory` y la conformidad.
- [x] **T4 · `bflow drop` y tareas retiradas fuera de la vista** (R4, R9, R10, R11, R12, R13). `Engine.Drop`, `Views`, `Panel`, `metrics.add`, statusline, doctor, comando `drop` y guard.
- [x] **T5 · Crear las hijas al aprobar el split** (R14, R15, R17, R18, R19, R20, R21, R23). `store.SplitChild`, `engine/split.go`, la validación en `apply`, `Approve` y `entry`.
- [x] **T6 · Oficio del spec-author** (R24). En `internal/agents/craft/spec-author.md`, la viñeta de SPLIT pasa a decir: "Si la feature no cabe en esos límites sin comprimir, no la comprimas. Escribe en el Brief una `### División` con una viñeta `- **<título>**: <alcance>` por hija (de 2 a 6, títulos de 120 caracteres como máximo) y reporta SPLIT con el motivo en `--note`. Al aprobarlo, bflow crea esas tareas." Correr `bflow render` para regenerar `.claude/agents/bflow-spec-author.md`. Si hay una prueba de contenido del oficio, se ajusta. README, guía y CHANGELOG quedan para el documenter.
