# GH-81 · Gate decision ofrece freeze --allow pero no lo aplica ni avisa que lo corre una persona

## Brief

**Objetivo:** cuando la decisión elegida necesita `bflow freeze --allow <archivo>`, bflow avisa en el gate y no relanza al agente hasta que una persona lo corre.

**Entra**
- Gate `decision`: la opción cuyo texto contiene `freeze --allow <archivo>` lleva en su `description` que antes de relanzar una persona corre ese comando en su terminal.
- Después de aprobar esa decisión, mientras el archivo siga congelado (no `allowed`), el `next` deja de ser `spawn` y pasa a `ask` con gate `freeze_allow`: pide a la persona correr el comando y ofrece "Ya lo corrí" (`bflow status <id>`) o bloquear la tarea.
- Cuando el archivo ya está `allowed`, el `next` vuelve a ser el `spawn` normal con la `decision` en los args.
- El prompt del implementer pide escribir el comando literal `bflow freeze --allow <archivo>` en la opción, para que bflow lo detecte.
- Guía (`docs/guia.md`): una línea sobre este paso.

**No entra**
- Que `approve` corra `freeze --allow` por su cuenta.
- Cambiar el guard: `bflow freeze` sigue negado a todo agente, sesión principal incluida.
- Que el panel muestre el gate `freeze_allow` (el panel lee `rec.Flow.Gate`, no el `next`).

**Decisiones nuevas**
- [N] Retener el `next` hasta que la persona corra el comando. Descartado: que aprobar la decisión registre el permiso. `approve` lo corre la sesión principal, y el guard deja fuera de `freeze` a la sesión principal a propósito; si `approve` diera el permiso, la sesión principal podría saltarse a la persona.
- [N] Calcular la retención en el engine (`withDisplay`, que alimenta `status` y la salida de cada comando) y no en `flow`: solo el engine conoce `frozen-tests.json`. `flow.State` no cambia.
- [N] Detectar el comando con una regex sobre el texto de `Decision`. Descartado: opciones con metadatos estructurados (`PendingGate.Options` pasaría de strings a structs, y eso cambia el formato de `report`). Si el agente no escribe el comando literal, el comportamiento es el de hoy.

**Riesgos**
- Falso positivo: la opción menciona el comando y el archivo no está congelado. Se ignora; solo se retiene si el archivo está en `frozen-tests.json` y no está `allowed`.
- La regex no captura rutas con espacios; las pruebas del repo no tienen espacios.

**Tamaño:** S (unas 120 líneas con pruebas; flow/next.go, engine/display.go, engine/freeze.go, agents.go, guía).

## Discovery

Carril light: no hay discovery. Caso real en GH-61 (`internal/cli/review_test.go`).

## Requirements

Carril light: los criterios van en Tasks.

## Design

- `flow.AllowRe = regexp.MustCompile("freeze\\s+--allow\\s+[`'\"]?([^\\s`'\"]+)")` y `flow.AllowFiles(text string) []string` (rutas normalizadas con `/`, sin duplicados, en orden de aparición).
- `gateNext`, caso `GateDecision`: si `AllowFiles(o)` no está vacío, `Option.Description = "Antes de relanzar al agente, una persona corre en su terminal: bflow freeze --allow <archivo>"` (una línea por archivo).
- `(*Engine).pendingAllow(rec store.Record) []string`: los archivos de `AllowFiles(rec.Flow.Decision)` que están en `readFrozen(id)` con hash distinto de `allowed`. Vacío si `Decision == ""`, si hay gate pendiente o si la fase no es `implementing`.
- `withDisplay`: si `n.Action == spawn` y `pendingAllow` no está vacío, devuelve `output.Next{Action: ask, Gate: "freeze_allow", Question: ..., Options: [{ID:"done", Label:"Ya lo corrí", Command:"bflow status <id>"}, {ID:"block", Label:"Dejarlo para después", Command:"bflow block <id> --reason \"falta bflow freeze --allow <archivo>\""}]}`. La pregunta dice que el guard no deja correrlo a un agente.
- Seguridad: no se agrega ninguna vía para que un agente marque `allowed`; el guard no cambia. La regex solo lee el texto que la persona ya eligió.

## Tasks

- [ ] T1 `flow`: `AllowRe`/`AllowFiles` y `Description` en las opciones de `GateDecision`. Criterios: CUANDO una opción contiene `` `bflow freeze --allow internal/x_test.go` `` su `Description` nombra ese archivo y el comando; CUANDO no lo contiene, `Description` queda vacía. Pruebas en `next_test.go` (`TestDecisionOptionWarnsFreezeAllow`) y `TestAllowFiles` (comillas, backticks, `\` en la ruta, varios archivos, ninguno).
- [ ] T2 `engine`: `pendingAllow` y la retención en `withDisplay`. Criterios: CUANDO se aprueba una decisión que pide `freeze --allow` de una prueba congelada, el `next` de `approve` y de `status` es `ask` con gate `freeze_allow` y no `spawn`; CUANDO una persona corrió `freeze --allow` de ese archivo, el `next` es el `spawn` del implementer con `decision`; CUANDO el archivo no está congelado o la decisión no menciona el comando, el `next` no cambia. Pruebas en `engine` (`TestDecisionHoldsUntilFreezeAllow`, `TestDecisionAllowUnknownFileNoHold`).
- [ ] T3 Prompt del implementer (`internal/agents/agents.go`) y `docs/guia.md`: la opción de NEEDS_DECISION que cambia una prueba congelada incluye el comando literal `bflow freeze --allow <archivo>`; la guía explica el gate `freeze_allow`. Correr `bflow render` si el prompt generado está versionado. Criterio: la prueba de agents que revisa el texto del implementer pasa con la nueva frase.
