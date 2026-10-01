# GH-32 · panel cierra las tareas que el tracker ya da por hechas o cuyo PR se mergeó, en cualquier fase

## Brief

**Objetivo:** que `bflow panel` cierre en local (fase `done`, sin gate) toda tarea empezada que el tracker ya da por terminada o cuyo PR se mergeó, sin importar la fase en que quedó la copia local.

**Entra**
- Revisión en `panel` de todas las tareas locales que no están en `done`, incluidas `blocked` y las que tienen un gate abierto (caso GH-13: `implementing` con `decision`).
- Dos motivos de cierre: el tracker dice `done` o cerrada (`tracker`), o el PR de la rama está mergeado, por número guardado o buscado por rama (`pr`).
- Evento nuevo en el log (`closed_outside`, con `from`, `to: done` y el motivo) y `stats` que no cuenta ese tramo como trabajo de agente ni espera humana.
- Mensaje en el panel: "GH-13 cerrada: terminada fuera de esta copia (tracker)" o "(PR #30 mergeado)".

**No entra**
- Llevar al tracker lo que va adelantado en local (eso ya lo hace sync).
- Relanzar agentes, comentar en el tracker o reabrir nada.
- Cambiar el cierre normal de `in_review` por merge (sigue siendo `merged`).

**Decisiones nuevas**
- [N] Evento propio `closed_outside` en `flow`, permitido desde cualquier fase empezada (también `blocked`). *Descartado:* aflojar `merged` para aceptar cualquier fase, porque `stats` lo confundiría con un cierre normal.
- [N] Con motivo `tracker` no se toca el tracker y se descartan los efectos pendientes. *Descartado:* reintentarlos, porque un `tracker_state` viejo regresaría el issue a `implementing`, y en Plane una tarea cancelada pasaría a Done. Con motivo `pr`, el tracker pasa a `done` igual que en un merge normal; si falla, queda pendiente.
- [N] El cierre se revisa antes de sincronizar los pendientes de la tarea, por la misma razón.
- [N] `vcs.Host` gana `FindMergedPR(ctx, head)`: el `FindPR` de hoy solo busca PRs abiertos, así que no encuentra uno ya mergeado. *Descartado:* cambiar `FindPR` a `state=all`, porque `openPR` lo usa para reutilizar un PR y podría tomar uno cerrado.
- [N] Una tarea que el tracker tiene cerrada sin estar en `done` (en GitHub "not planned", en Plane cancelada) también se cierra con motivo `tracker`; el log guarda el estado que traía el tracker.

**Riesgos**
- Más llamadas por cada `panel` (que corre en el hook de sesión): un `Get` por cada tarea local que no aparece en la lista de abiertas y, sin número de PR, un `FindMergedPR` por rama. Se mitiga revisando el tracker primero y consultando el PR solo si el tracker no la dio por hecha.
- Si el tracker se equivoca y la marca hecha, se pierde la fase local. El log guarda la fase de origen y `state.json` no se borra.

**Tamaño:** M. Toca flow (evento), engine (panel y un método de cierre), vcs y el adaptador de GitHub, metrics, el render del CLI, pruebas y el changelog.

## Discovery

No aplica (carril light).

## Requirements

En el carril light, los criterios van en cada tarea de **Tasks**.

## Design

- `flow.EvClosedOutside EventKind = "closed_outside"`. En `Event` se agrega `Reason string` con `json:"reason,omitempty"` (`"tracker"` o `"pr"`). `tx.apply`: lo acepta en estado `Blocked`. Rechaza `Backlog` con `not_started` y `Done` con `task_done`, como hoy. Llama a `t.enter(Done)`; con `Reason == "tracker"`, `t.moved = false` para no emitir `FxTrackerState`. También limpia `s.Block`.
- `engine.(*Engine).closeOutside(ctx, rec store.Record, reason string, pr *vcs.PR, trackerState string) (bool, string)`. Hace su propio `Store.Update` (sin `syncFirst`), aplica `flow.Apply`, corre `runEffects` y escribe una entrada `store.Entry{Event:"closed_outside", From, To:"done", Data:{"reason", "pr"?, "tracker_state"?}}`. Con `reason == "tracker"` vacía `rec.Pending`; con `"pr"` marca `rec.PR.Merged = true` y guarda número y URL. Devuelve si cerró y un aviso.
- `Panel`: la lista de abiertas (`OpenOnly`) se guarda por ID con `Phase` y `Closed`. Por cada registro que no está en `done`:
  1. Revisa el tracker. Si la tarea está en la lista, mira su `Phase`; si no está y la lista no falló, la pide con `Tracker.Get`. Con `Phase == done` o `Closed`, cierra con motivo `tracker`. Un `ErrNotFound` u otro error deja un aviso y no cierra.
  2. Si no, revisa el PR. En `in_review` se usa el `checkMerged` de hoy, que llama a `Merged`. En las demás fases: con `rec.PR.Number > 0` se usa `PRStatus`; si no, con `rec.Branch != ""` se usa `FindMergedPR`. Mergeado: cierra con motivo `pr`. Cerrado sin merge: el aviso de hoy, sin cerrar. `ErrNoCredentials` o `ErrNoPR`: nada.
  3. Solo después corre `Sync` de los pendientes, como hoy.
- `PanelReport.Closed []string` se queda (compatibilidad JSON). Se agrega `ClosedOutside []ClosedOutside` con `json:"closed_outside,omitempty"`, donde `ClosedOutside{ID, From flow.Phase, Reason string, PR int}`.
- `vcs.Host.FindMergedPR(ctx, head string) (PR, error)`: devuelve `ErrNoPR` si no hay. GitHub: `GET pulls?head=owner:head&state=closed`, el primero con `merged_at`. `fakeHost` lo implementa.
- `metrics.Compute`: con `Event == "closed_outside"` no atribuye a ninguna fase el tramo previo y pone `TaskStats.ClosedOutside string` con `json:"closed_outside,omitempty"` (el motivo).
- `renderPanel`: una línea por cierre de afuera, `"<ID> cerrada: terminada fuera de esta copia (tracker)"` o `"(PR #N mergeado)"`, sin emojis nuevos.
- Seguridad: no hay entrada nueva del usuario. Todo viene del tracker o del host con las credenciales que ya existen. No se escribe en el tracker más allá del `done` del motivo `pr`. Los errores de red quedan como avisos y nunca cierran.

## Tasks

- [ ] T1 · Evento `closed_outside` en `flow` (R1-R3)
  - R1: CUANDO llega `EvClosedOutside` en cualquier fase empezada que no sea `done` (incluidas `blocked` y una con gate abierto), el sistema DEBE dejar `phase=done`, `gate=nil` y `block=nil`.
  - R2: CUANDO `Reason == "tracker"`, el sistema NO DEBE emitir `FxTrackerState`; CUANDO `Reason == "pr"`, DEBE emitirlo hacia `done`.
  - R3: CUANDO la tarea está en `backlog` o en `done`, el sistema DEBE rechazar con `not_started` o `task_done`.
  - Pruebas en `internal/flow/machine_test.go`: `TestClosedOutsideFromAnyPhase`, `TestClosedOutsideTrackerEmitsNoEffect`, `TestClosedOutsideRejectsBacklogAndDone`.
- [ ] T2 · `FindMergedPR` en `vcs.Host`, el adaptador de GitHub y `fakeHost` (R4)
  - R4: CUANDO la rama tiene un PR cerrado y mergeado, `FindMergedPR` DEBE devolverlo; CUANDO solo hay PRs abiertos o cerrados sin merge, DEBE devolver `ErrNoPR`.
  - Prueba en `internal/adapters/vcs/github/github_test.go`: `TestFindMergedPR`. Se agrega `FindMergedPR` al mapa de credenciales/acceso que ya existe.
- [ ] T3 · `closeOutside` y la reconciliación en `Panel` (R5-R10)
  - R5: CUANDO el tracker da la tarea por `done` o cerrada y la copia local está en otra fase, `panel` DEBE cerrarla con motivo `tracker` y listarla en `closed_outside`.
  - R6: CUANDO el PR de la rama (por número guardado o por `FindMergedPR`) está mergeado y la tarea no está en `in_review`, `panel` DEBE cerrarla con motivo `pr`, guardar el número y mover el tracker a `done`.
  - R7: CUANDO el PR está cerrado sin merge, `panel` NO DEBE cerrar la tarea y DEBE dar el aviso de hoy.
  - R8: CUANDO cierra con motivo `tracker`, el sistema DEBE descartar los efectos pendientes sin reintentarlos y NO DEBE llamar a `Tracker.Transition` ni a `Tracker.Comment`.
  - R9: CUANDO cierra desde afuera, el log DEBE tener una entrada `closed_outside` con `from`, `to: done` y `data.reason`.
  - R10: CUANDO el tracker o el host fallan, `panel` NO DEBE cerrar nada y DEBE dar un aviso. El cierre de `in_review` por merge sigue igual (`merged`).
  - Pruebas en `internal/engine/vcs_test.go`: `TestPanelClosesTaskDoneInTracker`, `TestPanelClosesTaskWithMergedPRInAnyPhase`, `TestPanelDropsPendingWhenTrackerDone`, `TestPanelKeepsTaskWhenPRClosedUnmerged`. `TestPanelClosesMergedAndRemindsSLA` debe seguir verde.
- [ ] T4 · `stats` distingue el cierre de afuera (R11)
  - R11: CUANDO el log tiene `closed_outside`, `metrics.Compute` NO DEBE sumar a ninguna fase el tramo previo y DEBE poner `ClosedOutside` con el motivo.
  - Prueba en `internal/metrics/metrics_test.go`: `TestClosedOutsideNotCountedAsWork`.
- [ ] T5 · Salida del CLI y documentación (R12)
  - R12: CUANDO `panel` cierra tareas desde afuera, el texto DEBE mostrar una línea por tarea con el motivo y el JSON DEBE traer `closed_outside`. Esto también aplica al hook de sesión, que usa `renderPanel`.
  - Actualizar `internal/cli/flowcmds.go` (resumen del comando y `renderPanel`), `docs/guia.md` (línea 34), la fila de `panel` en `README.md` y `CHANGELOG.md`.
