# GH-129 · Desbloquear exige nota en todos los bloqueos, no solo en los de decisión manual

## Brief

**Objetivo:** que `next` pida nota al desbloquear solo cuando el bloqueo viene de `approve --gate decision --manual`.

**Entra**
- Marcar en el estado que un bloqueo viene de una decisión manual.
- `NextFor` (caso `Blocked`): con bloqueo manual, `unblock` sigue con `NeedsNote: true` y `--note "<resultado>"`; con bloqueo normal, `unblock` sin nota (`bflow unblock <id>`).
- Pruebas de ambos casos.

**No entra**
- Cambiar `bflow unblock` en la CLI: `--note` ya es opcional y `--no-changelog` sigue exigiéndola.
- La sugerencia `unblock --no-changelog --note` de `internal/engine/review.go`.

**Decisiones nuevas**
- [N] Campo `Manual bool` en `BlockInfo` (`json:"manual,omitempty"`), puesto por `GateDecision` al bloquear. Descartado: deducirlo del prefijo del motivo ("la sesión principal debe hacer esto..."), porque se rompe si el texto cambia.
- [N] En un bloqueo normal se ofrece una sola opción `unblock` sin nota. Descartado: dos opciones (con y sin nota), porque la mayoría de los bloqueos normales no tienen resultado que pasar, y quien quiera dar una nota puede correr `--note` a mano.

**Riesgos**
- Los estados que ya están bloqueados por una decisión manual no tienen el campo, así que caerán en la opción sin nota. Es aceptable: la nota sigue siendo posible a mano y el motivo del bloqueo ya dice qué hacer.

**Tamaño:** S (types.go, machine.go, next.go y pruebas).

## Discovery

Carril light: sin discovery.

## Requirements

## Design

## Tasks

- [x] T1 Modelo y máquina: agregar `Manual bool \`json:"manual,omitempty"\`` a `BlockInfo` (`internal/flow/types.go`). En `GateDecision` con `ev.Manual` (`internal/flow/machine.go`), después de `t.block(...)` sin error, poner `s.Block.Manual = true`. `tx.unblock` no cambia (borra `Block`).
  - Criterio: CUANDO se aprueba `decision` con `--manual`, el estado bloqueado DEBE tener `Block.Manual == true`; CUANDO se bloquea con `bflow block`, DEBE quedar en `false`.
- [x] T2 `NextFor`, caso `Blocked` (`internal/flow/next.go`): si `s.Block != nil && s.Block.Manual`, la opción `unblock` queda como hoy (`NeedsNote: true`, `cmd("unblock", s.ID, \`--note "<resultado>"\`)`, etiqueta que dice que el agente ve el resultado). Si no, `unblock` queda con `NeedsNote: false`, `Command: cmd("unblock", s.ID)` y etiqueta "Desbloquear y continuar". `keep` no cambia. Las opciones siguen siendo `unblock,keep`.
  - Criterio: CUANDO la tarea está bloqueada por un bloqueo normal (o sin `Block`), `next` DEBE ofrecer `unblock` sin `NeedsNote` y sin `--note` en el comando; CUANDO el bloqueo viene de una decisión manual, DEBE ofrecerla con `NeedsNote: true` y el marcador `<resultado>`.
- [x] T3 Pruebas (`internal/flow/next_test.go`): `TestBlockedUnblockNote` con dos casos: el estado que resulta de `Apply(EvBlock)` con motivo da `unblock` con `NeedsNote == false` y comando `bflow unblock T-1`; el que resulta de `Apply(EvApprove, GateDecision, Manual: true)` da `NeedsNote == true` y `--note`. Extender `TestManualDecisionBlocksInsteadOfRelaunching` para que verifique `res.State.Block.Manual`. Las pruebas que ya existen (`TestNextOptionsAreExecutable`, tabla `blocked`, e2e) deben seguir pasando.
