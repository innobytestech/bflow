# GH-7 · Agente bflow-scout que lee el repo antes de preguntar

## Brief

**Objetivo:** al empezar una tarea, un agente barato y de solo lectura (`bflow-scout`) deja en `reports/scout.md` lo que el repo ya contesta, para que discovery, spec e implementación partan de ahí.

**Entra**
- Agente `scout` (haiku, low) con oficio propio; corre una vez por tarea al entrar a la primera fase del carril (discovery en full, spec en light, implementing en hotfix).
- `bflow report <id> --agent scout --verdict DONE --stdin`: bflow guarda el reporte (≤40 líneas) en `.bflow/tasks/<id>/reports/scout.md`.
- Mientras el scout no reporta, la primera fase espera: no se abre el gate de discovery ni se aceptan reportes de los agentes de la fase.
- `bflow show <id> scout`; la skill (discovery), spec-author e implementer parten de ese reporte.
- `flow.scout: false` lo apaga; activo por defecto.

**No entra**
- Mapa de símbolos / paquete de contexto (`bflow map`, `context.json`): v0.2.
- Intake y `bflow new` (parte A de #4).
- Correr el scout en tareas que ya estaban empezadas al actualizar bflow.

**Decisiones nuevas**
- [N] El estado guarda `scout: pending|done`, puesto solo por `start`. Descartado: derivarlo de "fase = primera del carril y sin reporte", que dispararía el scout en tareas en curso y al volver a la primera fase.
- [N] El scout se activa en `config.Flow.Core()`, no en `flow.DefaultConfig()`. Descartado: ponerlo en DefaultConfig, que obliga a reescribir casi todas las pruebas de `internal/flow`.
- [N] Mientras el scout está pendiente, el reporte de otro agente de la fase se rechaza con `scout_pending`. Descartado: aceptarlo y dejar al scout como opcional.
- [N] `--stdin` solo lo acepta el scout; con otro agente se rechaza (`stdin_scout_only`). Descartado: guardar stdin para cualquier agente.
- [N] La lectura de stdin tiene un tope de 64 KiB. Descartado: leerla sin límite.

**Riesgos**
- Apagar `flow.scout` con una tarea que espera al scout: esa tarea sigue pidiendo `bflow-scout`, que ya no se genera. Hay que terminarla antes de apagarlo o reportar el scout a mano.
- Las pruebas de engine y cli que arrancan una tarea con la config del repo cambian: ahora el primer paso es el spawn del scout.
- Bash puede escribir: el límite de solo lectura lo pone el contrato, no la herramienta.

**Tamaño:** M. Cambia flow (estado, report, NextFor, Upcoming), engine (report con contenido, show, nudge), cli (`--stdin`), config, agents (catálogo, contrato, craft) y la skill del líder.

## Discovery

Scout de solo lectura (haiku/low) que corre una vez al entrar a la primera fase del carril, también en hotfix, y deja `reports/scout.md` (≤40 líneas). Clave propia en `flow.Config`, fuera de `Agents`; veredicto propio `DONE`; reporte por `--stdin` y bflow escribe el archivo. Rechazos: vacío, >40 líneas, fuera de momento, ya reportado. Activo por defecto, `flow.scout: false` lo apaga. `internal/flow` sigue puro.

## Requirements

**Estado y flujo (`internal/flow`)**
- R1 [D] CUANDO una tarea empieza (`EvStart`) y `Config.Scout` no está vacío, el sistema DEBE marcar `State.Scout = "pending"` antes de entrar a la primera fase del carril.
- R2 [D] CUANDO la tarea entra a discovery con `State.Scout == "pending"`, el sistema NO DEBE abrir `GateDiscovery`.
- R3 [D] MIENTRAS `State.Scout == "pending"` y la tarea no esté bloqueada, `NextFor` DEBE devolver un `spawn` de un solo agente `scout` (subagente `bflow-scout`), con args `id`, `lane` y `phase`, y `Report` igual a `bflow report <id> --agent scout --verdict DONE --stdin`.
- R4 [D] CUANDO el scout reporta `DONE` con `State.Scout == "pending"`, el sistema DEBE poner `State.Scout = "done"` y, si la fase es discovery, abrir `GateDiscovery`. En otra fase, el siguiente paso es el spawn normal de la fase.
- R5 [D] CUANDO el scout reporta con `State.Scout == "done"`, el sistema DEBE rechazar con `already_reported`.
- R6 [D] CUANDO el scout reporta con `State.Scout` vacío (tarea sin scout o empezada antes de esta versión), el sistema DEBE rechazar con `scout_not_due`.
- R7 [D] CUANDO el scout reporta un veredicto distinto de `DONE`, el sistema DEBE rechazar con `unexpected_verdict`.
- R8 [N] CUANDO otro agente reporta con `State.Scout == "pending"`, el sistema DEBE rechazar con `scout_pending` y decir que primero corre el scout.
- R9 [D] El scout NO DEBE aparecer en `Config.Agents`, `AgentNames`, `PhasesOf` ni contar para `allReported`.
- R10 [D] CUANDO la tarea vuelve a su primera fase (un rechazo o una decisión), el sistema NO DEBE volver a pedir el scout.
- R11 [D] MIENTRAS `State.Scout == "pending"`, `Upcoming` DEBE devolver lo que sigue al scout: `{discovery, GateDiscovery}` en discovery, o `{fase, Agents: cfg.Agents[fase]}` en otra fase.

**Reporte y lectura (`internal/engine`, `internal/cli`)**
- R12 [D] CUANDO llega `bflow report ... --agent scout --stdin`, el sistema DEBE leer stdin (64 KiB como máximo) y, si el flujo acepta el reporte, escribir el contenido en `.bflow/tasks/<id>/reports/scout.md` dentro de la misma transacción del estado.
- R13 [D] CUANDO el contenido del scout está vacío o solo tiene espacios, el sistema DEBE rechazar con `scout_empty` (código 2) sin cambiar el estado ni escribir el archivo.
- R14 [D] CUANDO el contenido del scout tiene más de 40 líneas (sin contar los saltos de línea del final), el sistema DEBE rechazar con `scout_too_long` (código 2) y decir cuántas líneas trae y que recorte y reporte otra vez.
- R15 [N] CUANDO un agente distinto del scout manda `--stdin`, el sistema DEBE rechazar con `stdin_scout_only` (código 2).
- R16 [D] CUANDO se pide `bflow show <id> scout`, el sistema DEBE devolver `reports/scout.md` y, si no existe, salir con código 0 y el texto `(<id> no tiene reporte del scout)`.
- R17 [N] CUANDO `bflow-scout` termina sin reportar y su reporte está pendiente, `Nudge` DEBE pedirle que siga, con el comando `--verdict DONE --stdin`, igual que a los demás agentes (mismo límite `MaxNudges`).

**Configuración y render**
- R18 [D] `config.Flow` DEBE aceptar `scout: false`. Sin la clave o con `true`, `Core()` DEBE poner `Scout = "scout"`.
- R19 [D] CUANDO el scout está activo, `agents.scout` NO DEBE dar el aviso "no trabaja en ninguna fase". Con el scout apagado, el aviso sí se da.
- R20 [N] CUANDO `flow.agents` pone `scout` en alguna fase, la validación DEBE dar el error `flow.agents: scout es un agente reservado; se apaga con flow.scout: false`.
- R21 [D] CUANDO el scout está activo, `agents.Build` DEBE generar `bflow-scout` con herramientas `read` y `bash` (sin `write`), modelo `haiku` y esfuerzo `low` por defecto, que se pueden cambiar con `agents.scout` (model, effort, read, extra, omit_claude_md).
- R22 [D] El contrato de `bflow-scout` DEBE decir que no escribe archivos, que reporta con `bflow report <id> --agent scout --verdict DONE --stdin` (contenido por stdin, ≤40 líneas) y qué hacer ante el código 2.
- R23 [D] Los renders de Claude (Read, Grep, Glob, Bash) y de OpenCode (read y bash, sin edit) DEBEN incluir `bflow-scout` sin cambios en el adaptador.
- R24 [D] La sección discovery de la skill del líder y los oficios de spec-author e implementer DEBEN pedir partir de `bflow show <id> scout` y preguntar o leer solo lo que el reporte no contesta. El contrato común DEBE listar `scout` entre los artefactos de `bflow show`.

## Design

**`internal/flow`**
- `types.go`: `type ScoutStatus string` con `ScoutPending ScoutStatus = "pending"` y `ScoutDone = "done"`. `State.Scout ScoutStatus \`json:"scout,omitempty"\``. Constante `ScoutAgent = "scout"`. Los estados guardados sin la clave quedan con `""`: no corre (R6, sin migración).
- `config.go`: `Config.Scout string` (nombre del agente; vacío = apagado). `DefaultConfig()` no cambia.
- `machine.go`:
  - `start`: si `t.cfg.Scout != ""`, poner `s.Scout = ScoutPending` antes de `t.enter(phases[0])` (R1).
  - `enter(Discovery)`: abrir `GateDiscovery` solo si `s.Scout != ScoutPending` (R2). Ninguna otra transición toca `s.Scout` (R10).
  - `report`: primero, `if ev.Agent == ScoutAgent { return t.scoutReport(ev) }`. Después, `if s.Scout == ScoutPending { reject("scout_pending", ...) }` (R8). El resto no cambia.
  - `scoutReport`: comprueba `done` → `already_reported`, `!= pending` → `scout_not_due`, veredicto `!= DONE` → `unexpected_verdict`. Si pasa, `s.Scout = ScoutDone` y, si `s.Phase == Discovery`, `s.Gate = &PendingGate{Name: GateDiscovery}` (R4–R7). No toca `Reports` (R9).
  - `Verdicts` no cambia. Se agrega `ScoutVerdicts = []Verdict{DoneV}`.
- `next.go`: en `NextFor`, después de los casos terminales y de Blocked, `if s.Scout == ScoutPending { return scoutNext(s) }` (R3). `Upcoming`: con `s.Scout == ScoutPending`, devolver `enter(s.Phase)` si la fase es discovery y `Step{Phase: s.Phase, Agents: cfg.Agents[s.Phase]}` si no (R11).
- Pendiente se decide por el estado, no por la config: así una tarea que espera al scout no se atora si la config cambia a medias (ver el riesgo del Brief).

**`internal/engine`**
- `ReportOpts.Content *string`: nil = sin `--stdin`.
- `Report`: si `Content != nil` y `Agent != scout`, `stdin_scout_only` (R15). Si `Agent == scout`, antes de `flow.Apply`: si está vacío, `scout_empty`; con más de 40 líneas (`strings.Count(strings.TrimRight(c, "\n"), "\n")+1`), `scout_too_long` (R13, R14). Todos son `*flow.Rejection`.
- Escritura: dentro del closure de `apply`, después de un `flow.Apply` sin error, `Store.WriteFile(id, "reports/scout.md", content)`. Si falla, devuelve el error y el estado no se guarda (R12). Para eso `apply` recibe un hook opcional `afterApply func(*store.Record) error`; también se puede abrir un camino `reportScout` propio. Se elige el hook para no duplicar `apply`.
- `spec.go`: `artifacts["scout"] = "reports/scout.md"`. En `Show`, si es `scout` y el archivo no existe, devuelve `"(<id> no tiene reporte del scout)"` sin error (R16). Se actualiza el mensaje de "disponibles".
- `nudge.go`: el agente cuenta como pendiente si es `scout` y `s.Scout == ScoutPending`. En `nudgeReason`, el comando para el scout es `--verdict DONE --stdin` (R17).

**`internal/cli`**
- `report`: bandera `--stdin` (bool). Con ella se lee `io.ReadAll(io.LimitReader(c.Stdin, 64<<10))` y se pasa en `Content` (R12). Si llega más del límite, `scout_too_long`. Se actualiza el texto de uso de `show` y de `report`.

**`internal/config`**
- `Flow.Scout *bool \`yaml:"scout,omitempty"\``. `Core()`: si es nil o true, `core.Scout = flow.ScoutAgent` (R18).
- Validación: en el switch de avisos, `case name == flow.ScoutAgent && core.Scout != "":` no avisa (R19). Si alguna lista de `c.Flow.Agents` contiene `scout`, da el error de R20.

**`internal/agents`**
- `catalog["scout"] = {"Lee el repo y resume lo que toca la tarea antes del discovery o la spec.", "haiku", "low", nil, false, false}`. `craft/scout.md` lleva el oficio: archivos, símbolos, pruebas y specs previas (`specs/`) que toca la idea; rutas y nombres exactos, sin opiniones ni propuestas; ≤40 líneas en viñetas.
- `Build`: después del bucle, si `fc.Scout != ""`, se agrega el Spec del scout: `Tools: {Read, Bash}`, `Body` con `scoutContract()` + oficio + oficio del repo (mismo `body`) (R21, R22). `scoutContract` es fijo: id/lane/phase; "No escribes archivos: bflow guarda tu reporte"; `bflow show <id> task`; `<pasted_content>`; report con `--stdin` vía heredoc; ≤40 líneas; código 2; respuesta final.
- `contract()`: la lista de `bflow show` suma `scout` (R24).
- Craft: `spec-author.md` e `implementer.md` agregan "Parte de `bflow show <id> scout`; lee solo lo que no conteste". `adapters/leader/bflow.md` § discovery: "Lee primero `bflow show <id> scout`; pregunta solo lo que el reporte y el repo no contestan" (R24).

**Seguridad**
- Authz: igual que hoy para `report`. No hay identidad de agente: cualquier proceso local puede reportar como scout, igual que cualquier otro agente.
- Validación: tope de 64 KiB al leer stdin, límite de 40 líneas y contenido no vacío. La ruta de escritura es fija (`reports/scout.md`, vía `Store.WriteFile`) y no la controla el agente.
- Datos sensibles: el reporte trae rutas y símbolos del repo, no secretos. Queda en `.bflow/`, que git ignora. El oficio prohíbe copiar valores de `.env` o credenciales.
- Inyección: el reporte del scout lo leen otros agentes. Sale de una tarea de terceros, pero no se envuelve en `<pasted_content>` porque lo escribe un agente propio. Riesgo bajo, aceptado.

**Descartado**
- Usar `--file` para el reporte del scout: tendría que escribir el archivo y necesitaría Write.
- Meter el scout en `Agents[primera fase]`: contaría en `allReported` y no se podría limitar a una vez por tarea.

## Tasks

- [ ] T1 Contrato: tipos, firmas y stubs (`ScoutStatus`, `State.Scout`, `Config.Scout`, `ScoutAgent`, `ScoutVerdicts`, `ReportOpts.Content`, `Flow.Scout`, `catalog["scout"]` y `craft/scout.md` vacío) y estas pruebas, que fallan contra los stubs:
  - flow: `TestScoutPendingOnStart`, `TestScoutDefersDiscoveryGate`, `TestScoutNextSpawn`, `TestScoutReportOpensDiscoveryGate`, `TestScoutReportLightThenSpecAuthor`, `TestScoutHotfixBeforeImplementer`, `TestScoutRejections` (already_reported, scout_not_due, unexpected_verdict, scout_pending), `TestScoutNotInAgentNames`, `TestScoutOncePerTask` (rechazo a spec en light), `TestScoutOffSkips`, `TestUpcomingWhileScoutPending`.
  - engine: `TestReportScoutWritesFile`, `TestReportScoutEmpty`, `TestReportScoutTooLong`, `TestReportStdinOnlyScout`, `TestShowScoutMissing`, `TestNudgeScout`.
  - config: `TestFlowScoutDefaultOnAndOff`, `TestAgentsScoutNoWarning`, `TestFlowAgentsScoutReserved`.
  - agents: `TestBuildScoutReadOnly`, `TestScoutContractStdin`.
  - cli: `TestReportStdinFlag`.
  (R1–R24)
- [ ] T2 Flujo en `internal/flow`: start, enter, report/scoutReport, NextFor, Upcoming. Las pruebas existentes siguen verdes, porque `DefaultConfig` no trae scout. Si hace falta, `TestTableCoversEveryReachableTransition` suma el estado del scout. (R1–R11)
- [ ] T3 Engine y CLI: `--stdin`, validaciones, escritura del archivo en la transacción, `show scout`, nudge. Se ajustan las pruebas de engine/cli que arrancan tareas con la config del repo. (R12–R17)
- [ ] T4 Config: `flow.scout`, `Core()`, avisos y error de nombre reservado. (R18–R20)
- [ ] T5 Agents y render: catálogo, `scoutContract`, `craft/scout.md`, Build y la lista de `show` en el contrato. Se comprueba que el render de Claude y el de OpenCode generan `bflow-scout` sin Edit/Write. (R21–R23)
- [ ] T6 Oficios: spec-author, implementer y la sección discovery de `adapters/leader/bflow.md` parten del reporte del scout. Se re-renderizan los agentes del repo. (R24)
