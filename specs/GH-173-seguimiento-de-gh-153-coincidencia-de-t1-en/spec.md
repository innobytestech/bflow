# GH-173 · Seguimiento de GH-153: coincidencia de T1 en rojo, mensajes de pausa y congelado tras Apply

## Brief

Objetivo: cerrar los 6 pendientes de GH-153 para que CONTRACT_READY avance sin gate solo cuando el último check demuestra que una T1 falló, y para que anuncios, mensajes y congelado digan la verdad.

Entra:
- Punto 1: coincidencia exacta de T1 en rojo (`internal/engine/contract.go`). Go: solo `--- FAIL: <Nombre>` con límite; `[build failed]`/`[setup failed]` en el paso test abre la gate contract. Otros: ruta completa del archivo con límites.
- Punto 2: `pause_late` con tarea bloqueada nombra la fase donde se bloqueó.
- Punto 3: retirada da `task_dropped`; se corrige R11 de la spec de GH-153 y `TestPauseLate`.
- Punto 4: `Upcoming` y las paradas del panel anuncian la gate contract cuando el repo no puede validar el contrato (sin checker o sin `test_step` entre los pasos).
- Punto 5: el congelado pasa después de `flow.Apply`; si la transición o sus efectos fallan, no queda congelado.
- Punto 6: prueba del error de `Contract.Last` como `contract_stale`.

No entra: predecir en Upcoming el caso dinámico (check que no confirma rojo); cambiar `DefaultFailPattern` o `check.Result`; tocar `next`/`notify` (ya no anuncian la gate); reescribir R5 de GH-153 (queda como historia).

Decisiones nuevas:
- [N] El modo Go se elige por archivo (T1 `.go`), no por `stack` de bflow.yaml. Descartado: `cfg.Stack == "go"`, que falla en monorepos y con stack vacío.
- [N] Un T1 `.go` nunca cuenta por su ruta, así `x_test.go:5: undefined` no cuenta. Descartado: ruta y nombre a la vez.
- [N] Cualquier `[build failed]`/`[setup failed]` en el paso test abre la gate aunque otra T1 tenga `--- FAIL`. Descartado: aceptar parcial por paquete.
- [N] A la izquierda de la ruta se acepta `/` (rutas absolutas en la salida), no letras, dígitos, `_`, `.` ni `-`. Descartado: exigir la ruta aislada (rompe con rutas absolutas).
- [N] Si el congelado falla tras `flow.Apply`, se reaplica el evento sobre el estado anterior con `ContractOK=false` (gate contract, como hoy). Si fallan los efectos, se restaura el congelado previo.
- [N] `flow.Config.ContractGate` lo calcula `Engine.flowCfg()`; el panel lo recibe en `View.ContractGate`. Descartado: pasar la config de check al paquete `flow`.

Riesgos: repos no Go cuya salida no imprime la ruta del T1 pasan a abrir la gate contract (más paradas humanas, no menos seguridad). Una subprueba Go fallida cuenta porque Go también imprime `--- FAIL: <Padre>`.

Tamaño: M; 6 tareas, sin migraciones ni cambios de permisos.

## Discovery

# Discovery GH-173

Alcance: los 6 puntos del issue (pendientes de GH-153 / #172).

1. Coincidencia de T1 en rojo (internal/engine/contract.go contractCheck/contractTestNames):
   - Go: una T1 cuenta como roja solo con la línea `--- FAIL: <NombreExacto>` (nombre completo, con límites; TestSum no coincide con TestSuma). Si la salida del paso test trae `[build failed]` o `[setup failed]` (error de compilación), no se afirma nada: verified=false sin rechazo, se abre la gate contract.
   - Otros stacks: coincidencia por ruta completa del archivo T1 (no nombre base) con límites de palabra; a_test.go no coincide con ba_test.go.
   - Si no se puede afirmar que falló por aserción contra los stubs, cae a la gate contract.
   - Pruebas requeridas: error de compilación que menciona archivo T1 abre gate contract; TestSum/TestSuma no coinciden; a_test.go/ba_test.go no coinciden.
2. internal/flow/machine.go tx.pause (~168): el mensaje de pause_late con tarea bloqueada muestra la fase real de la tarea, no "blocked".
3. R11: el resultado para tarea retirada es `task_dropped` (como ya hacen el código y consumer-changelog). Corregir R11 en specs/GH-153-05-.../spec.md y que internal/flow/pause_test.go acepte solo task_dropped.
4. Upcoming (internal/flow/next.go) y humanStops (internal/cli/ui.go): según la config. Si el repo no tiene test_step en check (o no hay check/validador de contrato), se anuncia la gate contract tras contract; si lo tiene, implementing. El caso dinámico (rojo no confirmado en ejecución) no se predice.
5. internal/engine/engine.go (~117-130): el congelado de T1 se mueve después de flow.Apply (hook after); si la transición falla no se congela.
6. Prueba nueva: error de Contract.Last convertido en contract_stale.

Restricciones: sin migraciones ni cambio de permisos; mantener convención TestContract* con mapas de casos.

## Requirements

- R1 [D] CUANDO un archivo T1 termina en `.go`, una línea de fallo del paso `test_step` DEBE contar como roja solo si, sin espacios al inicio, empieza con `--- FAIL: ` seguido del nombre exacto de una función `Test*` declarada en ese archivo y luego fin de línea, espacio o `/`.
- R2 [D] CUANDO un archivo T1 no termina en `.go`, una línea DEBE contar como roja solo si contiene la ruta completa del archivo (con `/`; la línea se normaliza de `\` a `/`) sin letra, dígito, `_`, `.` ni `-` justo antes o después; el nombre base solo no basta.
- R3 [D] CUANDO alguna línea de fallo (Fails o Tail) del paso `test_step` contiene `[build failed]` o `[setup failed]`, `contractCheck` DEBE devolver verified=false sin rechazo, y la tarea DEBE quedar en contract con la gate `contract` abierta y sin pruebas congeladas.
- R4 [D] `TestSum` NO DEBE coincidir con `--- FAIL: TestSuma`, ni `a_test.go`/`web/a.spec.ts` con `ba_test.go`/`web/ba.spec.ts`.
- R5 [D] CUANDO el paso `test_step` falló y ninguna línea cumple R1 o R2 (y no aplica R3), el sistema DEBE rechazar con `contract_green` como hoy, listando hasta 5 nombres T1.
- R6 [D] CUANDO se corre `bflow pause` con la tarea bloqueada desde quality o una fase posterior, el rechazo `pause_late` DEBE nombrar esa fase (`Block.From`), no `blocked`.
- R7 [D] CUANDO se corre `bflow pause` sobre una tarea retirada, el sistema DEBE rechazar con `task_dropped`; R11 de `specs/GH-153-05-carril-full-quitar-las-gates-de-contrato-y/spec.md` y `TestPauseLate` DEBEN decir solo eso.
- R8 [D] CUANDO el repo no puede validar el contrato (`Engine.Contract` nil o `check.test_step`, por defecto `test`, ausente de `check.steps`), `Upcoming` DEBE anunciar la gate `contract` tras la fase contract y el panel DEBE marcar contract como parada humana; CUANDO sí puede, DEBE anunciar implementing y no marcarla.
- R9 [D] El sistema DEBE congelar las T1 solo después de que `flow.Apply` aceptó CONTRACT_READY y sacó la tarea de contract; CUANDO `flow.Apply` rechaza el evento, NO DEBE escribir `frozen-tests.json`.
- R10 [N] CUANDO el congelado falla tras `flow.Apply`, el sistema DEBE dejar la tarea en contract con la gate `contract` y el aviso "no se pudieron congelar las pruebas" (como hoy).
- R11 [N] CUANDO los efectos de la transición fallan después de congelar, el sistema DEBE dejar `frozen-tests.json` como estaba antes (o borrarlo si no existía).
- R12 [D] CUANDO `Contract.Last` devuelve error, el sistema DEBE rechazar con `contract_stale` con el texto del error en la razón, sin cambiar la fase.

## Design

### Punto 1: `internal/engine/contract.go`

```go
// contractT1 son los nombres con que una línea de fallo nombra una prueba T1.
type contractT1 struct {
	Funcs []string // funciones Test* de los archivos T1 .go
	Paths []string // rutas (con /) de los archivos T1 que no son .go
}
func contractTestNames(root string, files []string) contractT1 // reemplaza al []string actual
func (c contractT1) names() []string                            // Funcs y luego Paths, para el mensaje contract_green
func (c contractT1) failed(line string) bool                    // R1, R2
func buildFailed(lines []string) bool                           // R3
func (e *Engine) contractValidable() bool                       // Contract != nil y test_step en check.steps
```

- `failed` con Funcs: `strings.TrimSpace(line)` tiene prefijo `--- FAIL: ` + nombre y lo que sigue es vacío o empieza con ` ` o `/`. Sin regex armadas con nombres.
- `failed` con Paths: normaliza `\` a `/` en la línea; recorre cada aparición con `strings.Index` y revisa el byte anterior y el siguiente contra `[A-Za-z0-9_.-]`.
- `contractCheck` usa `contractValidable()` en lugar de su cálculo actual; en el paso test, primero `buildFailed(failLines(s.Fails, s.Tail, -1))` devuelve `false, nil`; luego `failed`.
- `goTestFunc` se mantiene. `TestContractTestNames` se ajusta: los `.go` aportan Funcs y no su nombre base; `web/a.spec.ts` va en Paths; `falta_test.go` (no se puede leer) no aporta nada.

### Punto 2: `internal/flow/machine.go: tx.pause`
El `reject("pause_late", ...)` formatea la variable local `phase` en vez de `s.Phase`.

### Punto 3
R11 de la spec de GH-153 queda: "CUANDO se corre `bflow pause <id>` con la tarea en quality o una fase posterior, el sistema DEBE rechazar con `pause_late`; retirada, con `task_dropped`; sin empezar, con `not_started`." `TestPauseLate` acepta solo `task_dropped`.

### Punto 4
- `flow.Config.ContractGate bool`: el repo no puede validar el contrato, así que CONTRACT_READY abre la gate contract.
- `Engine.flowCfg()`: `fc := e.Cfg.Flow.Core(); fc.ContractGate = !e.contractValidable(); return fc`.
- `flow.Upcoming`, `case Contract` sin gate: con `cfg.ContractGate` devuelve `Step{Phase: Contract, Gate: GateContract}`; si no, como hoy.
- `engine.View.ContractGate bool` (`json:"-"`), llenado junto a `Upcoming` en `commands.go`.
- `internal/cli/ui.go`: el mapa `humanStops` pasa a `func humanStop(p flow.Phase, contractGate bool) bool` (contract es parada solo con `contractGate`); `buildPanel` lo llama con `v.ContractGate`.

### Punto 5: `internal/engine/engine.go: applyWith`
- El bloque de CONTRACT_READY solo calcula `verified`, fija `ev.ContractOK` y guarda `freeze := verified`.
- Tras `flow.Apply` y el hook `after`: si `freeze && res.State.Phase != flow.Contract`, `prev := e.readFrozen(id)` y `e.freezeTests`. Si falla: el aviso de hoy, `ev.ContractOK = false` y `res, err = flow.Apply(fc, before, ev)` (abre la gate). Si no: el aviso "N prueba(s) congelada(s)".
- Si `runEffects` falla con el congelado hecho: `e.restoreFrozen(id, prev)` antes de devolver el error. Nueva `func (e *Engine) restoreFrozen(id string, prev map[string]string)`: con `prev == nil` borra el archivo (`Store.Path` + `os.Remove`); si no, `saveFrozen(id, prev)`.
- La entrada del log usa el `ev` final, coherente con la fase.

### Punto 6
`fakeContract` gana `err error` y `Last` lo devuelve; `TestContractStale` suma el caso "error de Last".

### Seguridad
Sin cambios de authz ni de datos sensibles. Las líneas del check se tratan como texto: no se compilan regex con contenido de archivos ni de la salida. Ante duda (compilación, salida ambigua) se cae a la decisión humana, nunca a avanzar.

## Tasks

- [ ] T1 Contrato: firmas de Design (`contractT1`, `failed`, `names`, `buildFailed`, `contractValidable`, `restoreFrozen`, `flow.Config.ContractGate`, `View.ContractGate`, `humanStop`) con cuerpos stub, y pruebas en rojo: `TestContractRedMatch` (mapa: `--- FAIL: TestSuma (0.00s)` sí; `--- FAIL: TestSuma/x` sí; `TestSum` contra `--- FAIL: TestSuma` no; `suma_test.go:5:2: undefined: Suma` no; `web/a.spec.ts` contra `web/ba.spec.ts` no; `a_test.go` contra `ba_test.go` no; ruta absoluta y ruta con `\` sí), `TestContractBuildFailedOpensGate` (error de compilación que nombra el T1 + `[build failed]`: gate contract, sin congelar), `TestContractNoFreezeWhenApplyFails` (gate contract ya abierta: `gate_pending` y sin `frozen-tests.json`), `TestRestoreFrozen`, caso "error de Last" en `TestContractStale`, `TestPauseLateBlockedPhase`, `TestUpcomingContractGate`, `TestHumanStopContract`; `TestPauseLate` acepta solo `task_dropped`. (R1-R12)
- [ ] T2 Coincidencia exacta y build failed en `contract.go`; ajustar `TestContractTestNames` y `TestContractGreenNoT1Names`. (R1-R5)
- [ ] T3 Mensaje de `pause_late` con la fase real y corrección de R11 en la spec de GH-153. (R6, R7)
- [ ] T4 `ContractGate` en `flow.Config`/`flowCfg`, `Upcoming`, `View` y `humanStop`. (R8)
- [ ] T5 Congelado después de `flow.Apply`: reaplicar con `ContractOK=false` si falla y `restoreFrozen` si fallan los efectos; `TestContractFreezeFailureOpensGate` sigue verde. (R9-R11)
- [ ] T6 `fakeContract.err` y el caso de `Last` con error. (R12)
