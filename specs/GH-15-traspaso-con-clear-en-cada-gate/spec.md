# GH-15 · Traspaso con /clear en cada gate

## Brief

**Objetivo:** que la conversación principal se pueda descartar en cada gate que hace avanzar la fase y la sesión limpia retome la tarea en pocas líneas, sin perder decisiones humanas.

**Entra**
- Campo `clear` en `next`: lo trae la respuesta de un `approve` que cambia la fase.
- La skill bflow, al ver `clear`, para y sugiere /clear (en OpenCode, sesión nueva) y seguir con /bflow.
- `bflow hook session-start` agrega el `next` de la tarea activa en JSON compacto y la indicación "sigue con /bflow".
- El plugin de OpenCode inyecta esa misma salida al crearse una sesión principal.
- Una nota en `approve --note` (cualquier gate) queda en `decisions.md`, además de en `log.jsonl` como hoy.
- Prueba de traspaso: dogfooding manual con /clear en cada gate y comparación de tokens contra GH-15.

**No entra**
- Ejecutar /clear desde bflow (la sesión no puede hacerlo).
- `clear` en reject, report, status, gates sin cambio de fase (decision, rounds, questions).
- Volver obligatorio `--note` al aprobar.
- Prueba automatizada del ahorro de tokens.

**Decisiones nuevas**
- [N1] Con `clear`, la skill **para** tras sugerirlo; el humano sigue con /bflow (o dice "sigue" sin limpiar). Descartado: seguir solo, que hace el /clear inútil porque el trabajo de la fase siguiente cae en la conversación larga.
- [N2] El `next` del hook va **sin `display`** (el brief o el review-map pueden ser largos); /bflow vuelve a traerlo completo. Descartado: el `next` íntegro, que gasta los tokens que se buscan ahorrar.
- [N3] La nota de approve se escribe en `decisions.md` (lo que leen los agentes con `bflow show <id> decisions` y lo que va al PR). Descartado: solo `log.jsonl`, que ningún agente lee. Excepciones: gate `decision` (ya escribe su línea) y `questions` (sus respuestas son para el recorrido, ya viven en el estado).
- [N4] `clear` solo si además el `next` no es `done` (p. ej. split aprobado → tarea retirada: no hay nada que retomar).

**Riesgos**
- OpenCode: inyectar contexto sin respuesta depende de `client.session.prompt` con `noReply`; si la API no lo acepta, el plugin no inyecta nada (sin romper la sesión) y queda documentado.
- El plugin tiene tope de 100 líneas (hoy ~80).
- Contrato de salida: `clear` va con `omitempty`; los consumidores actuales no cambian.

**Tamaño:** mediano; ~7 archivos de Go/JS/MD, sin migración ni cambio de permisos.

## Discovery

Palanca 1 de tokens: descartar la conversación en cada gate y retomar en pocas líneas. `clear` (bool, omitempty) en `output.Next` solo tras un approve que avanza la fase. El hook de inicio agrega el `next` compacto; OpenCode inyecta lo mismo en `session.created` sin `parentID`. `approve --note` opcional queda en el historial. La prueba de traspaso es dogfooding manual con tokens de `bflow report`.

## Requirements

- **R1** [D] CUANDO `bflow approve` hace que la fase cambie (`Outcome.To` no vacío) y el `next` resultante no es `done`, el sistema DEBE devolver `next.clear = true`.
- **R2** [D] CUANDO `approve` no cambia la fase (gates `decision`, `rounds`, `questions`) o el comando es `reject`, `report`, `status`, `start` o `hook session-start`, el sistema NO DEBE poner `clear`.
- **R3** [D] El JSON de `next` DEBE omitir `clear` cuando es falso (`omitempty`).
- **R4** [N] CUANDO un approve avanza la fase pero el `next` es `done` (split aprobado, tarea retirada), el sistema NO DEBE poner `clear`.
- **R5** [D] CUANDO corre `bflow hook session-start` con una tarea activa, la salida de texto DEBE incluir la línea breve actual, una línea `next: <JSON compacto de una línea>` y la línea `Sigue con /bflow.`
- **R6** [N] El JSON compacto del hook DEBE omitir `display`; conserva `action`, `gate`, `skill`, `show`, `question`, `options`, `agents`, `parallel`, `reason`.
- **R7** [D] CUANDO no hay tarea activa o hay varias en curso, `hook session-start` DEBE comportarse como hoy (sin línea `next:`) y salir con 0 en todo caso.
- **R8** [D] CUANDO OpenCode emite `session.created` de una sesión sin `parentID`, el plugin DEBE correr `bflow hook session-start`, y si sale con 0 y con texto, agregarlo a esa sesión como contexto sin pedir respuesta al modelo.
- **R9** [D] CUANDO `bflow` no está en el PATH, el comando falla o la inyección falla, el plugin NO DEBE interrumpir a OpenCode ni avisar más de una vez.
- **R10** [N] CUANDO `approve` recibe `--note` no vacío en un gate distinto de `decision` y `questions`, el sistema DEBE agregar a `decisions.md` la línea `- <fecha> · gate <gate>: <nota en una línea> (<usuario>)`; si no se puede escribir, devuelve un warning y la aprobación sigue válida.
- **R11** [D] La nota de approve DEBE seguir registrada en `log.jsonl` (comportamiento actual, sin cambio).
- **R12** [D] La skill bflow DEBE decir: (a) si `next.clear`, avisar que la gate quedó aprobada, sugerir /clear (en OpenCode, sesión nueva) y seguir con /bflow, y no ejecutar ese `next` hasta que el humano diga [N1]; (b) si el humano añade un comentario o ratifica decisiones del brief al aprobar, pasarlo en `--note` del comando de la opción.
- **R13** [D] El walkthrough DEBE documentar una prueba de traspaso: una tarea recorrida con /clear en cada gate y la comparación de tokens de la sesión principal (`bflow report`) contra GH-15, que se hizo sin /clear.

## Design

**Contrato de salida** (`internal/output/output.go`)
```go
type Next struct {
    ...
    Clear  bool   `json:"clear,omitempty"` // la gate avanzó la fase: buen momento para /clear
    Reason string `json:"reason,omitempty"`
}
```

**Cuándo hay clear** (`internal/flow/next.go`), función pura para probarla sin engine:
```go
// SuggestClear dice si, tras un approve que llevó la tarea a la fase to
// (vacío si no cambió), conviene descartar la conversación.
func SuggestClear(to Phase, n output.Next) bool // to != "" && n.Action != output.ActionDone
```
`engine.Approve`, tras `apply` sin error: `out.Next.Clear = flow.SuggestClear(out.To, out.Next)`. Solo en `Approve`; `NextFor` no cambia, así `status` nunca trae `clear` (R2). Se decidió no meterlo en `NextFor` porque depende del evento, no del estado.

**Nota de approve en decisions.md** (`internal/engine/commands.go`): en `Approve`, tras el bloque de `GateDecision`, si `pending != nil`, `pending.Name` no es `decision` ni `questions` y `strings.TrimSpace(o.Note) != ""`:
`e.appendFile(id, "decisions.md", "# Decisiones en vuelo\n\n", fmt.Sprintf("- %s · gate %s: %s (%s)\n", fecha, pending.Name, oneLine(o.Note), e.User))`. Error → warning `no se pudo guardar decisions.md: ...`.

**Hook de inicio** (`internal/cli/hookcmds.go`): en `runSessionStart`, con tarea activa, después de `brief.Text`:
```go
func compactNext(n output.Next) string // n.Display = ""; json.Marshal sin escapar HTML; una línea
```
líneas: `"next: " + compactNext(n)` y `"Sigue con /bflow."`. `data` y `next` del envelope no cambian (el `--json` ya trae el `next` completo). Si `compactNext` falla al serializar, se omite la línea (nunca falla, R7).

**Plugin OpenCode** (`adapters/opencode/plugin.js`): en el `event` `session.created` con `!p.info.parentID` y `.bflow` presente, correr `bflow hook session-start` con stdout en `pipe` (helper `run` acepta el modo de stdout), timeout 10 s; si `code === 0` y hay texto: `client.session.prompt({ path: { id }, body: { noReply: true, parts: [{ type: "text", text }] } })`. Todo dentro del `try/catch` existente. Tope de 100 líneas.

**Skill** (`adapters/leader/bflow.md`): una viñeta nueva bajo la lista de acciones para `clear` (R12a) y una frase en `## approve` para `--note` (R12b). `README.md`: `clear` en la sección del contrato `next` y el hook de inicio con la línea `next:`.

**Descartado**: que bflow escriba un archivo de traspaso aparte (el estado y `decisions.md` ya lo son); `--note` obligatorio al aprobar; `clear` en `status` (se repetiría en cada lectura).

**Seguridad**: sin cambios de authz. La nota es texto del humano; `oneLine` la aplana para que no rompa el markdown de `decisions.md` ni el cuerpo del PR. El hook solo imprime estado local que ya imprime `bflow status`; no agrega datos sensibles. El plugin solo pasa la salida de bflow a la propia sesión, sin red.

## Tasks

- [x] **T1 Contrato** (R1-R13): `Next.Clear`; stub `flow.SuggestClear`; stub `compactNext`; pruebas que fallan contra los stubs:
  - `internal/flow/next_test.go`: `TestSuggestClear` (cambio de fase → true; sin cambio → false; next done → false).
  - `internal/engine/engine_test.go`: `TestApproveClearSoloAlAvanzar` (spec → contract trae clear; decision y questions no; reject no), `TestApproveNoteEnDecisions` (nota en gate spec escribe la línea; questions no escribe; sin nota no escribe).
  - `internal/output/output_test.go` o donde viva el JSON de Next: `TestNextClearOmitempty`.
  - `internal/cli/hookcmds_test.go` o `cmd/bflow/e2e_hook_test.go`: `TestSessionStartTraeNext` (línea `next:` sin `display`, línea `Sigue con /bflow.`, ≤10 líneas, exit 0; sin tarea activa no hay `next:`).
  - `internal/adapters/agent/opencode/plugin_test.go`: `TestPluginSessionStart` (el plugin trae `session-start`, `noReply`, filtra por `parentID`, ≤100 líneas).
  - `adapters/leader/leader_test.go`: `TestSkillReglaClear` (la skill menciona `clear`, /clear, sesión nueva y `--note` al aprobar).
- [x] **T2** `SuggestClear` y su uso en `engine.Approve` (R1-R4).
- [x] **T3** Nota de approve en `decisions.md` (R10, R11).
- [x] **T4** `compactNext` y líneas nuevas de `runSessionStart` (R5-R7).
- [x] **T5** Plugin de OpenCode con inyección en `session.created` y `bflow render` para regenerar (R8, R9).
- [x] **T6** Skill bflow y README (R12).
- [ ] **T7** Prueba de traspaso por dogfooding: tarea con /clear en cada gate, tokens de la sesión principal contra GH-15, anotada para el walkthrough (R13).
