# GH-16 · Presupuesto de tokens por fase

## Brief

**Objetivo:** que bflow avise y pregunte a la persona cuando una fase gasta más tokens nuevos de lo presupuestado, y que el guard frene a un subagente que repite la misma acción en un ciclo.

**Entra**
- `flow.budget` en bflow.yaml: mapa fase -> tokens nuevos (`200k`, `1.5M`, `200000`). Sin default.
- Al pasar 1x, 2x, 3x... del presupuesto, el siguiente next (report, approve, status, hook de inicio) es `ask` gate `budget` antes del next normal. Sí: sigue. No: la tarea se bloquea con el motivo en decisions.md.
- `bflow watch` muestra "142k / 200k nuevos" por fase con presupuesto; en color de aviso al pasarlo.
- `flow.loop_repeats`: el guard rechaza la N-ésima repetición idéntica (Bash, Read, Edit, Write) dentro de una corrida de un subagente. Sin default (apagado).

**No entra**
- Cortar o matar agentes en curso. Presupuesto por agente o por tarea completa. Cifras por defecto.
- Página web (`bflow ui`) y `bflow stats`: no cambian (el ciclo ya cuenta como bloqueo de guard).
- Ciclos de la sesión principal o de herramientas fuera de esas cuatro (Grep, Glob...).

**Decisiones nuevas**
- [N] "No sigue" bloquea la tarea (`blocked`, se retoma con `bflow unblock`). Descartado: la fase `paused`, que solo existe en full y ya significa "¿lanzar la revisión?".
- [N] La pregunta de presupuesto no es un gate de la máquina: se calcula al armar el next con los tokens del log, y lo respondido se guarda en el estado (`BudgetAck`). Descartado: abrirla como `State.Gate`, que pisaría el gate pendiente (decision, contract...).
- [N] Un Edit o Write permitido reinicia la cuenta de los Bash y Read repetidos de esa corrida: volver a correr las pruebas después de cambiar código no es un ciclo. Descartado: contar repeticiones crudas (falsos positivos con `go test` tras cada arreglo).
- [N] El ciclo se registra como evento `guard` con regla `loop`: suma en "bloqueos de guard" sin contador nuevo. Descartado: contador propio en métricas.
- [N] Rechazar el presupuesto también marca el múltiplo como respondido: al desbloquear no pregunta otra vez hasta el siguiente múltiplo.

**Riesgos**
- Los tokens llegan con los hooks Stop/SubagentStop: la pregunta sale en el siguiente next, no en el instante de cruzar.
- Con presupuesto configurado, cada next de una fase con presupuesto lee log.jsonl (sin presupuesto no se lee nada nuevo).
- Un umbral de ciclo bajo (2) puede frenar relecturas legítimas; la cifra la elige la persona.

**Tamaño:** M-L. 7 tareas; toca config, flow, engine, guard, adaptadores Claude y OpenCode, y watch.

## Discovery

# Discovery GH-16: presupuesto de tokens por fase + detección de ciclos

## Presupuesto por fase
- Config: `flow.budget` en bflow.yaml, mapa fase -> cifra de tokens nuevos. Acepta enteros o sufijos (200k, 1.5M). Fase desconocida o cifra inválida = error al cargar la config (doctor lo reporta).
- Default vacío: sin presupuesto en ninguna fase hasta tener cifras.
- Qué cuenta: Usage.New() (entrada sin caché + salida + escritura de caché) de todos los agentes y la sesión principal en esa fase, acumulado en todas las entradas de la tarea a la fase (re-entrar tras un rechazo suma, no reinicia).
- Momento: los tokens llegan al reportar; bflow no interrumpe agentes en curso. Si la fase pasó un umbral no respondido, el siguiente next (report, status, hook de inicio) es `ask` gate=`budget` ANTES del next normal; mostrar en display consumo vs presupuesto.
- Respuestas: Sí -> sigue, sale el next que tocaba, y vuelve a preguntar al cruzar 2x, 3x... del presupuesto. No -> la tarea pasa a paused con el motivo en decisions.md.
- Watch: en fases con presupuesto muestra siempre "142k / 200k nuevos"; en color de aviso al pasarlo (sin emojis). Fases sin presupuesto, como hoy.

## Detección de ciclos (palanca 4 del issue, dentro de alcance)
- Señal: la misma acción idéntica (misma herramienta con la misma entrada exacta: mismo comando Bash, mismo Read/Edit/Write con el mismo contenido/ruta) N veces dentro de una corrida de un subagente, vista desde el hook guard (PreToolUse; adaptadores Claude y OpenCode). Aplica solo a subagentes, no a la sesión principal.
- Reacción: guard rechaza la repetición N-ésima (y siguientes) con un mensaje al agente: está en un ciclo, que cambie de enfoque o reporte BLOCKED. Queda evento (cuenta en métricas como guarded o su propio contador) y aviso en watch. El agente sigue vivo.
- Umbral: `flow.loop_repeats` en bflow.yaml; vacío/0 = apagado (sin default hasta tener cifras).

## Restricciones
- No cortar el trabajo por presupuesto: solo avisar y preguntar.
- Salida para agentes mínima (JSON compacto); UI humana con texto en color, sin emojis.

## Requirements

Presupuesto
- R1 [D] CUANDO bflow.yaml trae `flow.budget`, el sistema DEBE aceptar por fase un entero o una cifra con sufijo `k` o `M` (decimal permitido: `1.5M`) y guardarla en tokens.
- R2 [D] CUANDO `flow.budget` nombra una fase que no es discovery, spec, contract, implementing, paused, quality, documenting o walkthrough, o la cifra no se puede leer o es ≤ 0, el sistema DEBE fallar al cargar la config con un mensaje que nombra la fase o la cifra (lo ve `bflow doctor`).
- R3 [D] CUANDO `flow.budget` falta o está vacío, el sistema DEBE comportarse como hoy: ni pregunta ni línea de presupuesto en watch.
- R4 [D] El gasto de una fase DEBE ser la suma de `Usage.New()` de todos los eventos `tokens` de la tarea en esa fase (agentes y sesión principal), acumulada sobre todas las entradas a la fase.
- R5 [D] CUANDO la tarea está en una fase con presupuesto, `k = floor(gasto / presupuesto) ≥ 1` y k es mayor que el último múltiplo respondido en esa fase, el sistema DEBE devolver como next un `ask` con gate `budget` antes de cualquier otro next (scout, gate pendiente o spawn).
- R6 [D] La pregunta de presupuesto DEBE traer en `display` la fase, el gasto contra el presupuesto y el múltiplo (p. ej. `implementing: 412k / 200k nuevos (2.1x)`), y dos opciones: seguir y pausar.
- R7 [D] CUANDO la persona responde `bflow approve <ID> --gate budget`, el sistema DEBE marcar k como respondido en esa fase, dejar sin cambios la fase y el gate pendiente, y devolver el next que tocaba.
- R8 [D] CUANDO el gasto de la fase alcanza el siguiente múltiplo (k+1) del presupuesto, el sistema DEBE preguntar de nuevo.
- R9 [D] CUANDO la persona responde `bflow reject <ID> --gate budget --note "<motivo>"`, el sistema DEBE bloquear la tarea con un motivo que incluye el gasto y la nota, y escribir una línea en decisions.md.
- R10 [N] CUANDO la persona rechaza el presupuesto, el sistema DEBE marcar k como respondido, de modo que tras `bflow unblock` no se pregunte otra vez hasta k+1.
- R11 [N] CUANDO llega `approve` o `reject` con gate `budget` y no hay pregunta de presupuesto vigente, el sistema DEBE rechazarlo con exit 2 y código `no_budget_gate`.
- R12 [N] CUANDO la persona aprueba el presupuesto, el sistema DEBE escribir una línea en decisions.md con la fase, el gasto y "sigue".
- R13 [N] Las entradas de log.jsonl de approve y reject de presupuesto DEBEN llevar `gate: "budget"`, aunque haya otro gate pendiente.
- R14 [D] CUANDO la tarea activa está en un carril con fases con presupuesto, `bflow watch` DEBE mostrar una línea `presupuesto` con `<fase> <gasto> / <presupuesto> nuevos` por cada una, y pintar en color de aviso (sin emojis) las que pasaron su presupuesto.
- R15 [D] El sistema NO DEBE interrumpir agentes en curso por presupuesto.

Ciclos
- R16 [D] CUANDO bflow.yaml trae `flow.loop_repeats`, el sistema DEBE aceptar 0 (apagado) o un entero ≥ 2; otro valor DEBE fallar al cargar la config.
- R17 [D] CUANDO `loop_repeats = N ≥ 2` y un subagente intenta por N-ésima vez (o más) en la misma corrida la misma acción, con la misma herramienta y la misma entrada exacta, el guard DEBE rechazarla (exit 2) con el motivo: está en un ciclo, que cambie de enfoque o reporte BLOCKED.
- R18 [D] La detección DEBE aplicar solo a subagentes (`Subagent` verdadero y sesión no vacía) y solo a Bash, Read, Edit y Write, en los adaptadores Claude y OpenCode.
- R19 [N] CUANDO el guard permite un Edit o Write de un subagente, DEBE reiniciar la cuenta de los Bash y Read de esa corrida.
- R20 [D] CUANDO el guard rechaza por ciclo, DEBE registrar en la tarea activa un evento `guard` con regla `loop`, el agente, la herramienta y las repeticiones; `bflow watch` DEBE describirlo como ciclo.
- R21 [D] CUANDO `loop_repeats` es 0 o falta, el guard NO DEBE leer ni escribir nada de ciclos.
- R22 [N] Cualquier error al leer o escribir la cuenta de ciclos DEBE dejar pasar la acción (el guard es abierto).

## Design

### Config (`internal/config/config.go`)
- `type TokenCount int64` con `UnmarshalYAML(*yaml.Node) error`: acepta `200000`, `200k`, `1.5M` (sufijo `k`/`K` = 1e3, `m`/`M` = 1e6; decimal con punto; se trunca a entero). Cifra ilegible o ≤ 0: error `flow.budget: cifra inválida %q`.
- `Flow` gana `Budget map[string]TokenCount yaml:"budget,omitempty"` y `LoopRepeats int yaml:"loop_repeats,omitempty"`.
- `Config.Validate`: fase de `Budget` fuera de `flow.BudgetPhases` -> `flow.budget: fase desconocida %q (válidas: ...)`; `LoopRepeats < 0 || == 1` -> `flow.loop_repeats debe ser 0 (apagado) o ≥ 2 (es %d)`.
- `Flow.Core()` copia `Budget` a `core.Budget`.

### Flow (`internal/flow`)
- `types.go`: `GateBudget Gate = "budget"`; `State.BudgetAck map[Phase]int json:"budget_ack,omitempty"` (último múltiplo respondido por fase).
- `config.go`: `Config.Budget map[Phase]int64`; `var BudgetPhases = []Phase{Discovery, Spec, Contract, Implementing, Paused, Quality, Documenting, Walkthrough}`.
- `func BudgetMultiple(budget, used int64) int` = `used / budget` (0 si budget ≤ 0).
- `func BudgetDue(cfg Config, s State, used int64) (k int, ok bool)`: ok si la fase está en `cfg.Budget`, no es Done/Dropped/InReview/Blocked/Backlog, `k ≥ 1` y `k > s.BudgetAck[s.Phase]`.
- `machine.go`, `approve` y `reject`: con `ev.Gate == GateBudget` no pasan por `pendingGate`. El engine pone el múltiplo en `ev.Choice`. `ev.Choice < 1` o `≤ BudgetAck[Phase]` -> `reject("no_budget_gate", ...)`. Approve: `BudgetAck[Phase] = Choice`, nada más. Reject: exige nota, `BudgetAck[Phase] = Choice` y `t.block(ev.Note)` (la nota llega armada por el engine). `ev.To` con gate budget -> `reject("bad_target", ...)`.
- `clone` copia `BudgetAck`.

### Engine (`internal/engine/budget.go`, nuevo)
- `func (e *Engine) phaseUsage(id string, p flow.Phase) (int64, error)`: recorre `Store.Log(id)` y suma `New()` de los eventos `tokens` con `data.phase == p`. Se exporta `metrics.UsageOf` (hoy `usageOf`).
- `func budgetNext(id string, p flow.Phase, used, budget int64) output.Next`: `ask`, gate `budget`, `Display` = `<fase>: <Human(used)> / <Human(budget)> nuevos (<x.x>x)`, `Question` = `La fase <fase> pasó su presupuesto de tokens. ¿Sigue?`, opciones `continue` (`bflow approve <ID> --gate budget`) y `pause` (`bflow reject <ID> --gate budget --note "<motivo>"`, NeedsNote).
- `func (e *Engine) next(rec store.Record) output.Next`: si la fase no tiene presupuesto devuelve `flow.NextFor` sin leer el log; si lo tiene, calcula el gasto y con `BudgetDue` devuelve `budgetNext`. Error al leer el log: `flow.NextFor`.
- `fill` y `Status` usan `e.next(rec)` en lugar de `flow.NextFor` (el hook de inicio pasa por `Status`). `withDisplay` deja intacto un next con gate budget.
- `Approve`/`Reject` con `Gate == "budget"`: calculan gasto y k; sin `BudgetDue` -> `flow.Rejection{Code: "no_budget_gate"}`. Reject arma la nota `presupuesto de <fase> superado (<used> / <budget> nuevos): <motivo>`. Ambos agregan a decisions.md `- <fecha> · presupuesto <fase>: <used> / <budget> nuevos -> sigue|pausa: <motivo> (<usuario>)`; las ramas actuales de decisions.md por gate pendiente se saltan con gate budget.
- `entry()`: con `ev.Gate == GateBudget`, `en.Gate = "budget"`.
- `func (e *Engine) Budgets() map[flow.Phase]int64` para watch.

### Guard y adaptadores
- `guard.Action` gana `Fingerprint string json:"fingerprint,omitempty"`: sha256 hex de `<herramienta original> + "\x00" + <entrada cruda>`. Claude: `tool_name` + `tool_input` crudo (se agrega `Raw json.RawMessage` al parseo). OpenCode: `tool` + `args` crudo; las acciones de una misma llamada (patch) comparten huella.
- `internal/guard/loop.go` (nuevo, puro): `type LoopCount struct{ Tool string; N int }`, `type LoopState struct{ Counts map[string]LoopCount }` (JSON `tool`, `n`, `counts`); `func (s *LoopState) Observe(a Action, repeats int) (n int, deny bool)`: suma 1 a la huella, `deny = n >= repeats`; si es Edit/Write y no se niega, antes borra las cuentas de Bash y Read.
- `internal/cli/hookcmds.go`: `checkLoop(c *Ctx, cfg *config.Config, acts []guard.Action) (guard.Decision, bool)`, llamado en `runGuard` antes de `allReads` (las lecturas también cuentan) y solo con `cfg.Flow.LoopRepeats ≥ 2`. Toma la primera acción con `Subagent && Session != "" && Fingerprint != ""` y herramienta de las cuatro; una vez por llamada del hook. Estado en `.bflow/cache/loops/<sha256(session)[:16]>.json` bajo `store.LockFile` (2 s); al crear un archivo nuevo borra los de más de 7 días. Cualquier error: permitir.
- Rechazo: regla `loop`, motivo `repetiste la misma acción (<tool>) <n> veces en esta corrida: estás en un ciclo. Cambia de enfoque o reporta BLOCKED con lo que te falta.`; se registra con `logGuard`, que gana `tool` y `repeats` en `Data`.

### Watch (`internal/cli/watch.go`)
- `watchData.Budget map[flow.Phase]int64` (de `e.Budgets()`). En `renderWatch`, tras `tokens`: `label("presupuesto", ...)` con las fases del carril que tienen presupuesto, en orden del carril; gasto de `PhaseStats.Tokens.New()`; las pasadas en `cWarn = "1;31"`.
- `describeEvent`: `guard` con regla `loop` -> `ciclo: <agente> repitió <tool> <n> veces`.

### Seguridad
- Authz: approve/reject de budget son comandos de la persona como los otros gates; el guard ya impide que un subagente corra `bflow approve` (sin cambio).
- Validación: cifras y fases al cargar la config; `--gate budget` sin pregunta vigente da exit 2.
- Datos sensibles: el archivo de ciclos guarda solo huellas sha256 y nombres de herramienta, nunca comandos ni contenido. El evento `guard` trunca el comando con `truncateCmd`, como hoy.
- Errores: métricas y cuenta de ciclos fallan abiertas (R22, `e.next`).

## Tasks

- [x] T1 Contrato (R1-R22): tipos y firmas de Design sin lógica (`TokenCount`, `Flow.Budget`, `Flow.LoopRepeats`, `flow.GateBudget`, `State.BudgetAck`, `Config.Budget`, `BudgetPhases`, `BudgetMultiple`, `BudgetDue`, `Engine.next`, `budgetNext`, `Engine.Budgets`, `Action.Fingerprint`, `LoopState.Observe`, `checkLoop`) y estas pruebas, que fallan:
  - config: `TestTokenCountParse`, `TestBudgetUnknownPhase`, `TestLoopRepeatsInvalid`, `TestBudgetEmptyByDefault`
  - flow: `TestBudgetDue`, `TestApproveBudgetKeepsGate`, `TestRejectBudgetBlocks`, `TestBudgetGateWithoutDue`
  - engine: `TestNextAsksBudgetFirst`, `TestBudgetAsksAgainAtNextMultiple`, `TestBudgetRejectUnblockNoReask`, `TestBudgetDecisionsLine`, `TestBudgetEntryGate`, `TestNoBudgetNoLogRead`
  - guard y adaptadores: `TestLoopObserveDeniesAtN`, `TestLoopEditResetsBash`, `TestClaudeFingerprint`, `TestOpenCodeFingerprint`
  - cli: `TestGuardLoopDeniesSubagent`, `TestGuardLoopIgnoresMain`, `TestGuardLoopOff`, `TestWatchBudgetLine`, `TestWatchLoopEvent`
- [x] T2 Config: `TokenCount`, `budget` y `loop_repeats` con validación y `Core()` (R1, R2, R3, R16).
- [x] T3 Flow: `GateBudget`, `BudgetAck`, `BudgetDue` y approve/reject de budget en la máquina (R5, R7, R8, R10, R11).
- [x] T4 Engine: gasto por fase, `e.next` en `fill` y `Status`, approve/reject con decisions.md y `entry()` (R4, R5, R6, R9, R12, R13, R15).
- [x] T5 Huella en los adaptadores Claude y OpenCode, y `LoopState.Observe` (R17, R18, R19).
- [x] T6 Guard: `checkLoop` en `runGuard` con archivo por corrida, poda, falla abierta y registro (R17, R18, R20, R21, R22).
- [x] T7 Watch: línea `presupuesto` con color de aviso y descripción del evento de ciclo (R14, R20).
