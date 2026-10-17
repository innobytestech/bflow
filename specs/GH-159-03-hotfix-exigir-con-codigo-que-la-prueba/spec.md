# GH-159 · [03] Hotfix: exigir con código que la prueba reproduzca el defecto antes del fix

## Brief

**Objetivo:** en hotfix, `report DONE` del implementer exige con código un commit previo de solo pruebas cuyo `bflow check` falló en el paso de pruebas; si no, código 2 `repro_test_missing`.

**Entra**
- Historial de check por commit: `checks/<sha>.json` en la carpeta de la tarea; `check.json`/`check.md` siguen siendo el último.
- Clave `check.test_step` (por defecto `test`) y verificación de la evidencia en el DONE de implementing en hotfix.
- `report --no-repro "<motivo>"`: abre el gate humano nuevo `repro`; aprobar avanza y deja el motivo en decisions.md, log y `bflow stats`; rechazar vuelve al implementer con la nota.
- Oficio del implementer, docs (`conceptos`, `configuracion`, `comandos`, `guia`) y CHANGELOG.

**No entra**
- Carriles light y full (sin cambios).
- Saber si la prueba que falla reproduce *ese* defecto (lo juzga el reviewer), ni exigir que siga existiendo en HEAD.
- Poda del historial de checks.
- Repos sin pasos de check: sin `check.steps` no hay check que comparar y la regla no aplica (igual que `check_required`).

**Decisiones nuevas**
- [N] La regla vive en el engine (lee git y el historial) y el gate en flow (puro). Descartado: todo en flow, que no hace I/O.
- [N] Rango de commits: `base..HEAD` con la base de los diffs (`vcs.base_branch`); si no hay base configurada, desde el HEAD guardado al crear la rama (`StartSHA` nuevo en el registro). Descartado: solo `base_branch`, que dejaría fuera a los repos sin esa clave.
- [N] Se descartan como evidencia HEAD, los merges y los commits vacíos. Descartado: aceptar un merge, cuyo diff contra el padre vale vacío y "solo pruebas" por vacuidad.
- [N] Gate propio `repro`. Descartado: reusar `decision`, que trae opciones del agente y cuenta como decisión en métricas.
- [N] Aprobado un `repro`, el motivo queda en el estado y las rondas de corrección no lo vuelven a pedir. Descartado: preguntarlo en cada ronda.
- [N] El historial solo guarda corridas con SHA limpio (no `DIRTY`).

**Riesgos**
- Rompe `TestDoneRequiresVigentCheck` (hotfix con pasos de check): hay que agregarle un commit de reproducción.
- Un agente puede poner como reproducción una prueba que falla por otra causa; el código no lo distingue.
- Las tareas hotfix en vuelo con `check.steps` necesitan evidencia o `--no-repro` en su próximo DONE.

**Tamaño:** M (flow, engine, check, adaptador git, metrics, CLI, docs; ~500 líneas con pruebas).

## Discovery

﻿# Discovery GH-159

## Alcance
- Solo carril hotfix. En `report DONE` (fase implementing), además de los rechazos existentes, bflow exige evidencia de reproducción; si falta, rechaza con `repro_test_missing`.
- Evidencia válida: un commit en base..HEAD, anterior al fix (ancestro de HEAD y distinto de HEAD), cuyo diff contra su padre solo toque archivos de `guard.test_patterns`; que tenga guardado un check en el que el paso `check.test_step` haya fallado; y un check en HEAD que pase (lo que ya se exige hoy).
- El escape `--no-repro "<motivo>"` es para defectos que no se reproducen con una prueba (docs, config, carreras).

## Datos clave
- Historial de check por SHA: cada `bflow check` guarda su resultado por SHA dentro del directorio de la tarea (p. ej. `checks/<sha>.json`). `check.json` y `check.md` siguen siendo el último resultado, para no romper a quien los lee hoy. Se guarda todo el historial por tarea, sin poda.
- El implementer corre `bflow check` dos veces: en el commit de la prueba (falla) y en HEAD (pasa).
- Paso de pruebas: clave nueva `check.test_step`, por defecto "test". Solo cuenta el fallo de ese paso; los demás pasos del commit de prueba no importan.
- "Archivo de prueba" = `guard.test_patterns`, que ya existe (defaults por stack).

## Errores
- `repro_test_missing`: no hay commit de solo pruebas en base..HEAD, o lo hay pero no tiene check guardado, o su check no falló en `check.test_step`. El `reason` dice cuál de los tres casos es y cómo arreglarlo (correr `bflow check` en ese commit).
- Si `check.test_step` no existe entre los pasos configurados, la evidencia no vale y el reason lo dice.

## Restricciones
- `--no-repro "<motivo>"` NO avanza la fase por sí solo: abre una gate `ask` y el humano aprueba o rechaza. Al aprobar, el motivo queda en decisions.md y en métricas. El agente no puede saltarse la regla solo.
- Los carriles light y full no cambian.
- E2E: hotfix sin commit de prueba previo → rechazado; con prueba que falla y luego pasa → aceptado; con --no-repro → gate humana.
- Actualizar la prosa de `internal/agents/craft/implementer.md` (regla de hotfix) para describir el flujo nuevo (dos corridas de check), y la documentación para consumidores (changelog, docs/conceptos.md).

## Requirements

- **R1 [D]** CUANDO `bflow check` corre con ID de tarea y el SHA no es `DIRTY`, el sistema DEBE guardar el resultado también en `.bflow/tasks/<ID>/checks/<sha>.json`, sin borrar los de otros commits, y DEBE seguir escribiendo `check.json` y `check.md` con el último resultado.
- **R2 [N]** CUANDO el SHA de la corrida es `DIRTY` o no hay ID de tarea, el sistema NO DEBE escribir en `checks/`.
- **R3 [D]** El sistema DEBE leer `check.test_step` de `bflow.yaml`, con `test` por defecto.
- **R4 [D]** CUANDO el implementer reporta DONE en implementing en el carril hotfix, hay pasos de check y git, y ninguna de las condiciones anteriores del DONE rechazó, el sistema DEBE rechazar con código 2 `repro_test_missing` salvo que exista un commit de evidencia (R5).
- **R5 [D]** Un commit es evidencia SI Y SOLO SI está en el rango de la tarea (R6), no es HEAD, no es un merge, su diff contra el padre no está vacío y todos sus archivos casan con `guard.test_patterns`, y su check guardado (R1) trae el paso `check.test_step` con estado `fail`.
- **R6 [N]** El rango de la tarea DEBE ser `<base>..HEAD` con la base de los diffs (`<remote>/<base_branch>`) si `vcs.base_branch` está configurada; si no, `<StartSHA>..HEAD`, con `StartSHA` el HEAD guardado al crear la rama. Sin ninguna de las dos, el DONE DEBE rechazarse con `repro_test_missing` y un reason que pida configurar `vcs.base_branch`.
- **R7 [D]** El `reason` de `repro_test_missing` DEBE decir cuál caso aplica y cómo arreglarlo: (a) no hay commit de solo pruebas en el rango; (b) los hay pero ninguno tiene check guardado (con sus SHA cortos y el comando para correr `bflow check <ID>` en ese commit); (c) tienen check pero el paso `check.test_step` no falló en ninguno; (d) `check.test_step` no es ningún paso de `check.steps`.
- **R8 [D]** CUANDO el implementer reporta DONE con `--no-repro "<motivo>"` en hotfix y pasan las demás condiciones del DONE (check vigente incluido), el sistema DEBE omitir R4, abrir el gate `repro` con el motivo y el agente, y NO DEBE cambiar de fase.
- **R9 [N]** CUANDO `--no-repro` llega vacío, con otro veredicto, en otra fase o en otro carril, el sistema DEBE rechazar con código 2 `no_repro_invalid`.
- **R10 [D]** CUANDO el humano aprueba el gate `repro`, el sistema DEBE pasar a la fase siguiente del carril, guardar el motivo en el estado, añadir una línea a `decisions.md`, registrar `data.no_repro` en la entrada del log y comentar en el tracker.
- **R11 [N]** CUANDO el humano rechaza el gate `repro` con `--note`, el sistema DEBE quedarse en implementing, cerrar el gate y reanudar al implementer con la nota.
- **R12 [N]** CUANDO la tarea ya tiene un `repro` aprobado, los DONE siguientes del implementer NO DEBEN exigir evidencia.
- **R13 [D]** `bflow stats <ID>` DEBE mostrar el motivo aprobado (`no_repro` en `--json`).
- **R14 [D]** En los carriles light y full el DONE NO DEBE cambiar.
- **R15 [N]** Mientras el gate `repro` está abierto, `bflow next` DEBE preguntar al humano con dos opciones: aceptar sin prueba y pedir la prueba (con nota).
- **R16 [D]** El oficio del implementer y la documentación para consumidores DEBEN describir el flujo de las dos corridas de check y el escape `--no-repro`.

## Design

### Flujo del implementer en hotfix

1. Commit con solo la prueba que reproduce el defecto → `bflow check <ID>` (falla en `test`; queda `checks/<sha>.json`).
2. Commit(s) con la corrección → `bflow check` (pasa) → `report DONE`.

Si el commit de prueba ya no es HEAD y le falta el check: `git switch --detach <sha> && bflow check <ID> && git switch -`.

### config (`internal/config/config.go`)

- `Check.TestStep string \`yaml:"test_step,omitempty"\`` — el paso cuyo fallo cuenta como reproducción. `applyDefaults`: `""` → `"test"`. Sin validación al cargar: si no casa con un paso, lo dice el DONE (R7d).

### check (`internal/check/store.go`)

- `Save(st, id, res)`: además, si `id != ""` y `res.SHA` no es `""` ni `"DIRTY"`, escribe `checks/<res.SHA>.json` con `st.WriteFile` (que crea la carpeta).
- `func LoadAt(st *store.Store, id, sha string) (*Result, error)` — nil, nil si no existe.
- `func (v Verifier) StepAt(ctx context.Context, id, sha, step string) (found bool, status string, err error)` — `found` false si no hay `checks/<sha>.json`; `status` es el `Status` del paso con `Name == step`, o `""` si el check no lo trae.

### vcs (`internal/vcs/vcs.go`) y adaptador git

- `Commits(ctx context.Context, base string) ([]string, error)` — `git rev-list --no-merges --reverse <base>..HEAD`.
- `CommitFiles(ctx context.Context, sha string) ([]string, error)` — `git diff-tree --no-commit-id --name-only -r <sha>`.
- El fake de `internal/engine/vcs_test.go` implementa los dos.

### store

- `Record.StartSHA string \`json:"start_sha,omitempty"\`` — HEAD tras `EnsureBranch` en `createBranch`. Las tareas viejas no lo tienen: dependen de `base_branch` (R6).

### flow

- `types.go`: `GateRepro Gate = "repro"`; `Event.NoRepro string \`json:"no_repro,omitempty"\`` (motivo, report DONE); `State.NoRepro string \`json:"no_repro,omitempty"\`` (motivo aprobado; `enter` no lo limpia).
- `report`: antes del `switch`, si `strings.TrimSpace(ev.NoRepro) != ""` y no se cumple (`Verdict == DoneV`, `Phase == Implementing`, `Lane == Hotfix`) → `reject("no_repro_invalid", ...)`. El vacío lo rechaza el engine (R9), porque flow no distingue el vacío de la ausencia.
- `case Implementing` (verdict DONE): tras `CheckOK`, si `ev.NoRepro != ""` → `s.Gate = &PendingGate{Name: GateRepro, Agent: ev.Agent, Note: ev.NoRepro}` y return sin `enter`.
- `approve` `GateRepro`: `s.NoRepro = g.Note`; comentario `**Hotfix sin prueba de reproducción (aprobado):** <motivo>`; `t.enter(t.cfg.after(s.Lane, Implementing))`.
- `rejectTargets[GateRepro] = {Implementing}`: el camino existente `to == s.Phase` cierra el gate, `Resume = true` y deja la nota.
- `next.go` `gateNext`: pregunta `"<agente> pide cerrar el hotfix sin prueba de reproducción: <motivo>. ¿Lo aceptas?"`; opciones `approveOpt(id, GateRepro, "Aceptar sin prueba")` y `rejectOpt(id, GateRepro, "test", "Pedir la prueba de reproducción", "")`.

### engine

- `ReportOpts.NoRepro *string` (nil: sin bandera; la CLI lo llena si `--no-repro` aparece, vía `fs.Visit`). `Report` rechaza `no_repro_invalid` si apunta a un texto en blanco; si no, pasa `strings.TrimSpace(*o.NoRepro)` a `Event.NoRepro`.
- Interfaz nueva:
  ```go
  // CheckHistory lee el check guardado de un commit.
  type CheckHistory interface {
  	StepAt(ctx context.Context, id, sha, step string) (found bool, status string, err error)
  }
  ```
  Campo `Engine.Checks CheckHistory` (nil: hotfix no exige prueba de reproducción). `cmd/bflow/wire.go` lo llena con el mismo `check.Verifier` cuando hay `check.steps` y git. El engine no importa `check`.
- `func (e *Engine) reproMissing(ctx context.Context, rec store.Record) *flow.Rejection` en `internal/engine/repro.go`: aplica R5–R7 en este orden: `test_step` existe en `Cfg.Check.Steps` (d) → base (R6) → `Commits` sin HEAD → candidatos con `CommitFiles` no vacío y todos `guard.MatchesTest(Cfg.Guard.TestPatterns, f)` (a) → `StepAt` de cada candidato: alguno `fail` → nil; ninguno `found` (b); si no (c). Errores de git o del store salen como reason del rechazo, no como error interno.
- En `applyWith`, bloque DONE de implementing, justo antes de `ev.CheckOK = true`: si `Lane == Hotfix && e.Checks != nil && e.Git != nil && ev.NoRepro == "" && rec.Flow.NoRepro == ""` → `reproMissing`.
- `Approve`: si `pending.Name == GateRepro`, línea en `decisions.md`: `- <fecha> · gate repro: hotfix sin prueba de reproducción: <motivo> (<user>)`.
- `entry`: en un approve que cierra `GateRepro`, `Data["no_repro"] = before.Gate.Note`.
- `createBranch`: guarda `rec.StartSHA`.

### CLI, metrics

- `report`: `fs.String("no-repro", "", "hotfix: cierra sin prueba de reproducción; abre un gate humano con el motivo")`.
- `metrics.TaskStats.NoRepro string \`json:"no_repro,omitempty"\``, de la entrada con `data.no_repro`; `stats <ID>` imprime `sin prueba de reproducción: <motivo>`.

### Superficie de seguridad

- **Authz:** el agente no avanza solo con `--no-repro`; solo `approve --gate repro` (humano) cierra. La evidencia vive en `.bflow/`, que el guard no deja escribir a los agentes con Edit/Write ni con redirecciones de Bash; quedan fuera los intérpretes (`python -c`), como ya documenta el guard.
- **Validación:** el SHA viene de `git rev-list`, no del usuario, y se usa como nombre de archivo: `store.Path` ya rechaza rutas fuera de la tarea. El motivo pasa por `oneLine` antes de decisions.md.
- **Errores:** fallos de git o de lectura del historial no dejan pasar el DONE (rechazo con reason), no lo convierten en error 1.
- **Datos sensibles:** ninguno nuevo; el historial guarda lo mismo que `check.json`.

## Tasks

- [ ] **T1 · Contrato** (R1–R15). Stubs que compilan: `Check.TestStep`, `check.LoadAt`, `Verifier.StepAt`, `vcs.Git.Commits/CommitFiles` (adaptador y fake), `Record.StartSHA`, `flow.GateRepro`, `Event.NoRepro`, `State.NoRepro`, `engine.CheckHistory`, `Engine.Checks`, `ReportOpts.NoRepro`, `reproMissing`, `TaskStats.NoRepro`. Pruebas que fallan contra los stubs:
  - `internal/check/store_test.go`: `TestSaveKeepsHistoryBySHA`, `TestSaveSkipsHistoryWhenDirty`, `TestStepAt`.
  - `internal/adapters/vcs/git/git_test.go`: `TestCommitsAndCommitFiles` (excluye merges, orden viejo→nuevo).
  - `internal/config/config_test.go`: `TestCheckTestStepDefault`.
  - `internal/flow/machine_test.go`: `TestNoReproOpensGate`, `TestApproveReproAdvances`, `TestRejectReproBackToImplementing`, `TestNoReproInvalid`.
  - `internal/flow/next_test.go`: fila `repro hotfix` (ask, gate `repro`, opciones `approve,test`).
  - `internal/engine/repro_test.go`: `TestReproMissing` (tabla: sin commits · solo código · prueba sin check · check que pasó · `test_step` inexistente · HEAD de solo pruebas no cuenta · commit vacío · base desde `StartSHA` · sin base ni `StartSHA` · evidencia válida), `TestReproSkippedAfterApproval`, `TestReproOnlyHotfix`.
  - `internal/metrics/metrics_test.go`: `TestComputeNoRepro`.
  - `cmd/bflow/e2e_repro_test.go`: `TestHotfixDoneWithoutReproRejected`, `TestHotfixReproFailThenPassAccepted`, `TestHotfixNoReproOpensHumanGate` (aprobar → quality, decisions.md, `stats`; rechazar → implementing con nota).
- [ ] **T2 · Historial de check y `test_step`** (R1, R2, R3): `Save` por SHA, `LoadAt`, `StepAt`, default de config.
- [ ] **T3 · Git y `StartSHA`** (R5, R6): `Commits`, `CommitFiles` en el adaptador; `createBranch` guarda `StartSHA`.
- [ ] **T4 · Gate `repro` en flow** (R8, R9, R10, R11, R12, R15): tipos, `report`, `case Implementing`, `approve`, `rejectTargets`, `gateNext`.
- [ ] **T5 · Regla en el engine** (R4–R9, R12, R14): `reproMissing`, enganche en `applyWith`, `ReportOpts`, `Engine.Checks` en `wire.go`, bandera `--no-repro` en la CLI. Adapta `TestDoneRequiresVigentCheck`: tras `start`, un commit que solo toque `a_test.go` con un check que falle en `test`, para que su último DONE siga llegando a quality.
- [ ] **T6 · Rastro del escape** (R10, R13): línea en `decisions.md`, `data.no_repro` en el log, `TaskStats.NoRepro` y la línea de `stats <ID>`.
- [ ] **T7 · Oficio y docs** (R16): `internal/agents/craft/implementer.md` (dos corridas de check, `--no-repro` y cuándo usarlo: docs, config, carreras); `bflow render`; `docs/conceptos.md` (hotfix), `docs/configuracion.md` (`check.test_step`), `docs/comandos.md` (`report --no-repro`, gate `repro`), `docs/guia.md` (fila `repro_test_missing` en «Cuando algo se atora»); entrada en `CHANGELOG.md` (Cambiado, con «hace falta correr `bflow render`»).
