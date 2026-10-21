# GH-153 · [05] Carril full: quitar las gates de contrato y pausa

## Brief

**Objetivo:** el carril full pasa de 7 a 5 gates humanas: el contrato se congela solo cuando bflow comprueba que las pruebas T1 fallan contra los stubs, y `implementing -> quality` es automático con DONE y check verde.

**Entra**
- CONTRACT_READY valida el contrato con el último `bflow check` guardado (pasos previos a `check.test_step` en verde, `test_step` en rojo nombrando pruebas T1) y, si pasa, congela y avanza a implementing sin gate.
- Rechazos nuevos `contract_stale`, `contract_broken` y `contract_green`, cada uno con el siguiente paso.
- `paused` sale de `DefaultLanes` del carril full.
- Comando nuevo `bflow pause <id>`: el próximo DONE de implementing se detiene en la gate `pause` de siempre.
- next, notify, watch, ui, texto de los agentes (re-render), `docs/conceptos.md`, `docs/guia.md`, `docs/comandos.md`, CHANGELOG.

**No entra**
- Quitar la fase contract (el implementer sigue escribiendo T1) ni cambiar `freeze --allow`.
- Cambios en `metrics` (ya mide la espera por gate).
- Que el engine corra el check: lee el que ya corrió el implementer.

**Decisiones nuevas**
- [N] La validación lee el check guardado y vigente de la tarea; no lo corre el engine. Descartado: correr los pasos dentro de `report` (lento, y duplica `bflow check`).
- [N] Si bflow no puede validar (sin `check.steps`, `test_step` que no está en los pasos o sin checker), abre la gate `contract` como hoy. Descartado: rechazar el CONTRACT_READY (dejaría sin salida a los repos sin check).
- [N] "Nombra pruebas T1": una línea de fallo del `test_step` contiene el nombre base de un archivo T1 o el nombre de una prueba Go declarada en él. Descartado: parsear la salida de cada framework.
- [N] La pausa es una marca en el estado (`PauseRequested`) que se consume al entrar a `paused`; al aprobarla se sigue con la fase posterior a implementing aunque el carril no tenga `paused`. Descartado: insertar `paused` en el carril de esa tarea.
- [N] `pause` se acepta en cualquier carril mientras la tarea no haya llegado a quality. Descartado: solo en full (no aporta y complica el mensaje).

**Compatibilidad:** las tareas que ya esperan en las gates `contract` o `pause` se aprueban igual que siempre. Un `lanes.full` explícito con `paused` conserva la pausa.

**Riesgos**
- En stacks no Go, un contrato que no compila puede fallar en `test_step` y pasar como "rojo" si ningún paso previo compila. Se mitiga en el texto del agente y la guía (build/vet antes del `test_step`).
- El implementer ahora debe commitear el contrato y correr `bflow check` antes de CONTRACT_READY; un check DIRTY o viejo se rechaza (`contract_stale`).

**Tamaño:** M (flow, engine, check, cli, agentes, docs; sin migración de datos).

## Discovery

# Discovery GH-153: carril full sin gates de contrato y pausa

## Alcance
- El carril full pasa de 7 a 5 gates humanas: carril, discovery, spec, decisiones en vuelo, preguntas + walkthrough.
- Se quita la gate `contract`: el contrato se congela automáticamente al recibir CONTRACT_READY si pasa la validación nueva. La fase Contract sigue existiendo (el implementer escribe el contrato); solo desaparece la aprobación humana.
- Se quita `paused` del carril full por defecto (DefaultLanes): `implementing -> quality` es automático con DONE y check verde.
- `freeze --allow` se conserva sin cambios para corregir una prueba congelada mal hecha.

## Validación del contrato al recibir CONTRACT_READY (antes de congelar)
1. Sin pruebas huecas (hollowTests, como hoy: `contract_hollow`).
2. Correr los pasos de check reutilizando el mecanismo de `test_step` de la reproducción de hotfix (GH-159), independiente del stack: los pasos anteriores al test_step (build/vet/compilación) deben pasar; el test_step debe fallar; y las líneas de fallo deben nombrar pruebas de los archivos T1 de la tarea (taskTests).
3. Rechazos nuevos con código propio y motivo accionable: si no compila/vet falla (p. ej. `contract_broken`), si las T1 pasan contra los stubs o el fallo no nombra pruebas T1 (`contract_green`).
4. Si todo pasa: congelar (freezeTests) y avanzar a implementing sin gate.

## Implementing -> quality
- Al recibir el DONE de implementing, el engine corre `check --verify` (mismo criterio que el comando: último check pasó y corresponde al código actual, sin congeladas modificadas). Si no, rechaza (`check_not_green`) y el implementer corrige. Si pasa, avanza directo a quality (lanza revisión).

## bflow pause <id>
- Comando nuevo, opcional: marca la tarea para que el próximo DONE de implementing se detenga en la gate `pause` en vez de lanzar la revisión. La gate pause existente (approve/reject) se reutiliza tal cual.

## Compatibilidad
- Las tareas que ya esperan en gate contract o pause se siguen aprobando con el comando de siempre.
- Un `lanes.full` explícito en bflow.yaml que incluya `paused` conserva la pausa (ya es configurable por carril).
- Solo cambia el valor por defecto y que CONTRACT_READY ya no abre gate.

## Superficie a tocar
- internal/flow: machine.go (report Contract, enter Paused, rejectTargets/decisionTargets), next.go, config.go (DefaultLanes), pruebas de transición (machine_test, next_test).
- internal/engine: engine.go (validación en Report), commands.go/freeze.go (freeze automático), engine_test; e2e en cmd/bflow.
- internal/cli: comando `pause`, notify.go/ui.go/watch.go (gateAction, humanStops) para no anunciar gates que ya no existen.
- internal/agents: texto de contract() y craft/implementer.md (ya no hay aprobación humana del contrato; CONTRACT_READY debe venir con T1 en rojo); re-render de agentes.
- Docs: docs/conceptos.md, docs/guia.md (tabla de gates), CHANGELOG.md, README si menciona las gates.

## Supuestos
- La validación con las cinco personas de prueba ya está hecha; las métricas de espera por gate (metricscmds) confirman la decisión después, sin cambios de código.

## Requirements

- R1 [D] CUANDO el implementer reporta CONTRACT_READY en la fase contract y el contrato tiene pruebas huecas, el sistema DEBE rechazar con `contract_hollow`, como hoy.
- R2 [N] CUANDO se reporta CONTRACT_READY y el último check guardado de la tarea no existe, es DIRTY o hay cambios en `check.code_paths` posteriores a su SHA, el sistema DEBE rechazar con `contract_stale` y pedir commitear el contrato y correr `bflow check <id>`.
- R3 [D] CUANDO algún paso de `check.steps` anterior a `check.test_step` terminó en `fail`, el sistema DEBE rechazar con `contract_broken`, nombrando el paso y hasta 10 de sus líneas de fallo.
- R4 [D] CUANDO el paso `check.test_step` pasó, o falló sin que ninguna línea de fallo nombre una prueba T1 (R5), el sistema DEBE rechazar con `contract_green`, diciendo que las T1 deben fallar contra los stubs.
- R5 [N] Una línea de fallo nombra una prueba T1 SI contiene el nombre base de un archivo de `taskTests` o el nombre de una función `Test*` de Go declarada en uno de esos archivos.
- R6 [D] CUANDO el contrato pasa R1-R4, el sistema DEBE congelar las pruebas de la tarea (como hoy al aprobar la gate) y llevar la tarea a la fase que sigue a contract sin abrir gate, con `resume` activo para el implementer.
- R7 [N] CUANDO no se puede validar (sin checker, `check.steps` vacío o `check.test_step` que no está entre los pasos), el sistema DEBE abrir la gate `contract` como hoy, y al aprobarla congelar las pruebas.
- R8 [D] El carril full por defecto DEBE ser discovery, spec, contract, implementing, quality, documenting, walkthrough, in_review, done (sin `paused`).
- R9 [D] CUANDO llega un DONE válido de implementing (check verde vigente, como hoy) y la tarea no pidió pausa, el sistema DEBE pasar a la fase siguiente del carril (quality en full).
- R10 [D] CUANDO una persona corre `bflow pause <id>` con la tarea en una fase anterior a quality (incluida implementing), el sistema DEBE marcarla para pausar; repetirlo no es error.
- R11 [N] CUANDO se corre `bflow pause <id>` con la tarea en quality o una fase posterior, el sistema DEBE rechazar con `pause_late`; retirada, con `task_dropped`; sin empezar, con `not_started`.
- R12 [D] CUANDO llega un DONE válido de implementing y la tarea tiene la marca de pausa, o el carril incluye `paused`, el sistema DEBE entrar a `paused` con la gate `pause` y quitar la marca.
- R13 [N] CUANDO se aprueba la gate `pause`, el sistema DEBE ir a la fase que sigue a `paused` en el carril o, si el carril no la tiene, a la que sigue a implementing; al rechazarla, vuelve a implementing como hoy.
- R14 [D] Las tareas que ya esperan en la gate `contract` o `pause` DEBEN poder aprobarse y rechazarse con los mismos comandos y destinos que hoy.
- R15 [D] `next`, `Upcoming`, `watch`, `notify` y `humanStops` NO DEBEN anunciar las gates contract ni pause para una tarea que no las va a abrir.
- R16 [D] El texto que generan los agentes para la fase contract DEBE decir que CONTRACT_READY exige el contrato commiteado y un `bflow check <id>` con las T1 en rojo, y que al aceptarse las pruebas quedan congeladas; lo de `freeze --allow` sigue igual.
- R17 [D] `docs/guia.md`, `docs/conceptos.md`, `docs/comandos.md` y `CHANGELOG.md` DEBEN describir el full con 5 gates, la validación automática del contrato y `bflow pause`.

## Design

### Lectura del check (internal/check)

- `func Fresh(ctx context.Context, g GitReader, codePaths []string, res *Result) (bool, string)`: lo que hoy comprueba `Verify` salvo el `PASS` (nil, DIRTY, código sucio, cambios desde el SHA). `Verify` llama a `Fresh` y luego exige `PASS`; su comportamiento no cambia.
- `func (v Verifier) Last(ctx context.Context, id string) (res *Result, fresh bool, detail string, err error)`: `Load` + `Fresh`.

### Validación del contrato (internal/engine)

- Interfaz nueva en `engine.go`:
  ```go
  // ContractChecker lee el último check de la tarea para validar el contrato.
  type ContractChecker interface {
      Last(ctx context.Context, id string) (res *check.Result, fresh bool, detail string, err error)
  }
  ```
  Campo `Engine.Contract ContractChecker // nil: el contrato pasa por la gate contract`. `cmd/bflow/wire.go` le asigna el mismo `check.Verifier` que a `Verifier`. `engine` importa `internal/check` (check no importa engine).
- Archivo nuevo `internal/engine/contract.go`:
  - `func (e *Engine) contractCheck(ctx context.Context, id string) (verified bool, rej *flow.Rejection)`. Orden: `contract_hollow` (el chequeo de hoy se mueve aquí); si no se puede validar (R7) devuelve `false, nil`; luego `contract_stale`, `contract_broken`, `contract_green`; si todo pasa, `true, nil`.
  - Los pasos previos al `test_step` cuentan como bien con `pass` o `skip`; los posteriores se ignoran.
  - `func contractTestNames(root string, files []string) []string`: nombres base de `files` más los nombres que captura `goTestStart` (hollow.go) en los `.go`. Se buscan como subcadena en `StepResult.Fails` y, si viene vacío, en las líneas de `Tail`.
- `Engine.applyWith`: con CONTRACT_READY en contract llama `contractCheck`; si hay rechazo lo devuelve; si no, `ev.ContractOK = verified`. Tras aplicar con éxito y con `verified`, llama `freezeTests` y agrega el aviso "N prueba(s) congelada(s): no se editan hasta el PR", igual que `Approve`.
- `Approve` sigue congelando al aprobar `GateContract` (R7, R14).

### Máquina de estados (internal/flow)

- `Event.ContractOK bool` (json `contract_ok,omitempty`): lo pone el engine cuando validó el contrato.
- `EvPause EventKind = "pause"` y `State.PauseRequested bool` (json `pause_requested,omitempty`).
- `tx.report`, caso `Contract`: con `ev.ContractOK`, `t.enter(t.cfg.after(s.Lane, Contract))` y `s.Resume = true`; sin él, `GateContract` como hoy.
- `tx.report`, caso `Implementing` (camino sin `NoRepro`): `next := t.cfg.after(s.Lane, Implementing)`; si `s.PauseRequested && next != Paused`, `t.enter(Paused)`; si no, `t.enter(next)`.
- `enter(Paused)`: además de abrir `GatePause`, pone `s.PauseRequested = false`.
- `tx.pause(ev)`: rechaza `pause_late` si la fase es quality o posterior en `Order`, o `dropped`; si no, `s.PauseRequested = true` sin cambiar de fase ni de gate. Sin estado previo, el engine ya devuelve `not_started`.
- `func (c Config) afterPause(l Lane) Phase`: `after(l, Paused)` si el carril tiene `paused`; si no, `after(l, Implementing)`. Lo usan `approve` (`GatePause`) y `Upcoming`.
- `DefaultLanes`: `Full` sin `Paused`; el comentario dice que `paused` es opcional (con `bflow pause` o `lanes` en bflow.yaml).
- `Upcoming`: en `Contract` sin gate devuelve los agentes de la fase siguiente; en `Implementing` con `PauseRequested` devuelve `{Paused, GatePause}`.
- `next.go`: la opción de `decisionTargets` hacia contract dice "Rehacer el contrato (al validarse se vuelven a congelar las pruebas)". Los casos `GateContract` y `GatePause` se quedan por R7 y R14.
- `rejectTargets` y `RejectTargets` no cambian: `GatePause -> Implementing` vale aunque el carril no tenga `paused`.

### CLI (internal/cli)

- `bflow pause [ID]` en `flowcmds.go`: `Engine.Pause(ctx, id) (Outcome, error)` aplica `flow.Event{Kind: flow.EvPause}` y responde el sobre de siempre, con un aviso de que la pausa llega cuando termine la implementación.
- `ui.go` (`humanStops`), `watch.go` y `notify.go`: anuncian `contract` solo si la tarea la abrió (R7) y `pause` solo si el carril incluye `paused` o la tarea tiene `PauseRequested`.

### Agentes y docs

- `agents.go` (`contract()`, bloque `flow.Contract`): "Antes de CONTRACT_READY, commitea pruebas y stubs y corre `bflow check <id>`: los pasos previos al de pruebas deben pasar y las T1 fallar. Al aceptarse, las pruebas quedan congeladas…" seguido del texto actual de `freeze --allow`. Lo mismo en `internal/agents/craft/implementer.md` si menciona la aprobación humana. Re-render de `.claude/agents/` y `.opencode/agents/`.
- `docs/guia.md`: tabla de gates sin "Contrato T1" ni "Pausa"; un párrafo con la validación automática, los rechazos `contract_*` y `bflow pause`. `docs/conceptos.md`: carriles, máquina de estados y cuenta de gates. `docs/comandos.md`: `pause`. `CHANGELOG.md`: entrada en la versión en curso.

### Seguridad y errores

- Authz: `pause` es para personas; no es veredicto de ningún agente y el guard lo bloquea a los subagentes igual que `approve` y `freeze`.
- Validación: un `check.test_step` desconocido no rompe nada, solo abre la gate (R7). Los rechazos nuevos se registran como `refused` en el log, como hoy.
- Datos sensibles: los motivos copian líneas de fallo del check, que ya se ven en `bflow show check`; se recortan a 10 líneas.

### Descartadas

- Correr el check dentro de `report` (lento y con efectos).
- Rechazar sin gate cuando no hay check configurado.
- Borrar `GateContract` y `GatePause` del código: rompería las tareas en vuelo y el respaldo de R7.

## Tasks

- [x] T1 Contrato: `check.Fresh`, `Verifier.Last`, `engine.ContractChecker`, `Engine.Contract`, `Engine.Pause`, `flow.EvPause`, `Event.ContractOK`, `State.PauseRequested`, `Config.afterPause` y stubs de `contractCheck`/`contractTestNames`. Pruebas en rojo: `TestFreshIgnoresResult`, `TestContractStale`, `TestContractBroken`, `TestContractGreen`, `TestContractGreenNoT1Names`, `TestContractTestNames`, `TestContractReadyFreezesAndAdvances`, `TestContractUnverifiableOpensGate`, `TestFullLaneNoPause`, `TestImplementingDoneGoesToQuality`, `TestPauseRequestStopsAtGate`, `TestPauseLate`, `TestPauseApproveWithoutPausedPhase`, `TestLegacyContractAndPauseGates`, `TestUpcomingPauseRequested`. (R1-R14)
- [x] T2 `internal/check`: `Fresh` y `Verifier.Last`; `Verify` delega en `Fresh` sin cambiar su comportamiento. (R2)
- [x] T3 `internal/engine/contract.go`: `contractCheck`, `contractTestNames`, rechazos `contract_stale|broken|green`, congelado tras el CONTRACT_READY aceptado; el chequeo de huecas se mueve aquí; wire en `cmd/bflow/wire.go`. (R1-R7)
- [x] T4 `internal/flow`: `DefaultLanes` sin `paused`, `ContractOK` en `report`, `PauseRequested`, `EvPause`/`tx.pause`, `afterPause` en approve y `Upcoming`; ajustar `machine_test`, `next_test` y `TestPausedConfigurablePerLane`. (R6-R14)
- [x] T5 `internal/cli`: comando `pause`, bloqueo en el guard para subagentes, `humanStops`, `watch`, `notify`; e2e en `cmd/bflow/e2e_test.go` del full sin gates contract/pause y con `pause`. (R10, R11, R15)
- [x] T6 Agentes: texto de `contract()` y `craft/implementer.md`, etiqueta de `decisionTargets`, re-render. (R16)
- [x] T7 Docs: `docs/guia.md`, `docs/conceptos.md`, `docs/comandos.md`, `CHANGELOG.md`. (R17)
