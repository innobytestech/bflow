# GH-54 · No hay forma de regresar a implementing desde documenting (reject da no_gate)

## Brief

**Objetivo:** que `bflow reject --to implementing --note "<motivo>"` regrese la tarea a implementing desde quality o documenting aunque no haya gate pendiente, con el motivo en la bitácora y en el tracker.

**Entra**
- `reject` sin gate pendiente en las fases `quality` y `documenting`: único destino `implementing` (por defecto si no se da `--to`), `--note` obligatorio.
- La tarea entra a implementing con `Resume=true` y `Note=<motivo>`, igual que un rechazo de walkthrough; el implementer retoma con el motivo.
- Comentario en el tracker `**Regresada a implementing desde <fase>:** <motivo>` y evento `reject` en la bitácora con `from`, `to` y `note` (sin `gate`).
- `watch` describe ese evento como «regresada» y no como «rechazada gate » vacío.
- Docs: `comandos.md` (fila de reject), quitar #54 de problemas conocidos en `como-probar-y-reportar.md`, CHANGELOG.

**No entra**
- Regresar a spec o contract sin gate (sigue la ruta de NEEDS_DECISION del agente).
- Fases con gate propio (paused, walkthrough): ya tienen `reject` por gate y no cambian.
- Otras fases sin gate (discovery, spec, contract, implementing): siguen dando `no_gate`.
- Cambiar `block`/`unblock`.

**Decisiones nuevas**
- [N] Reusar `reject` en vez de un comando nuevo (`bflow back`, `bflow reopen`). Descartado el comando nuevo: el criterio de la tarea acepta `reject --to implementing`, guard ya trata `reject` como decisión humana y no hay que tocar adaptadores ni skills.
- [N] Solo quality y documenting. Descartado «cualquier fase posterior a implementing»: paused y walkthrough ya tienen gate con destino implementing, e in_review/done quedan fuera del flujo de la rama.
- [N] Si se pasa `--gate` sin gate pendiente, sigue `no_gate`: el usuario apuntó a un gate que no existe.
- [N] La ronda de calidad no cambia al regresar: no es un rechazo de la compuerta y no debe disparar el cortacircuito `rounds`.

**Riesgos**
- Que `reject` sin gate se use por error en quality con reportes a medias: `enter` limpia `Reports`, así que los revisores vuelven a correr. Es lo esperado al regresar a implementing.

**Tamaño:** S. Un caso nuevo en `tx.reject` (internal/flow/machine.go), una línea en `describeEvent` (internal/cli/watch.go), pruebas y docs.

## Discovery

Carril light, sin discovery. Ver el reporte del scout (`bflow show GH-54 scout`).

## Requirements

<!-- carril light: los criterios van en Tasks -->

## Design

<!-- carril light: ver Brief y Tasks -->

## Tasks

- [ ] T1 · Regreso sin gate en la máquina de estados (internal/flow/machine.go, internal/flow/machine_test.go)
  - En `tx.reject`, antes de `pendingGate`: si `s.Gate == nil`, `ev.Gate == ""` y `s.Phase` está en `backTargets` (nuevo `var backTargets = map[Phase][]Phase{Quality: {Implementing}, Documenting: {Implementing}}`), llamar a un nuevo `t.back(ev)`. En cualquier otro caso, el camino actual sin cambios (`no_gate`, `gate_mismatch`, etc.).
  - `t.back(ev)`:
    - R1. CUANDO la nota está vacía, DEBE rechazar con `note_required`.
    - R2. CUANDO `ev.To` no está vacío y no es `implementing` (o el carril no tiene implementing), DEBE rechazar con `invalid_target` y listar los válidos.
    - R3. CUANDO es válido, DEBE hacer `t.enter(Implementing)`, conservar `s.Round` previo, poner `s.Resume = true`, `s.Note = <motivo>` y agregar el comentario `**Regresada a implementing desde <fase>:** <motivo>`.
    - R4. El efecto `FxTrackerState` sale con `Phase: implementing`; no se crea rama ni sello de inicio (ya existen).
  - R5. En spec/contract/implementing sin gate, `reject` sigue dando `no_gate`; con `--gate x` en documenting sin gate, también `no_gate`.
  - R6. Con gate pendiente en cualquier fase, el comportamiento de `reject` no cambia (las pruebas existentes siguen verdes).
  - Pruebas: `TestRejectBackToImplementingFromDocumenting`, `TestRejectBackToImplementingFromQuality` (con un reporte a medias: `Reports` queda vacío, `Round` igual), `TestRejectBackNeedsNote`, `TestRejectBackInvalidTarget`, `TestRejectWithoutGateElsewhereIsNoGate`.
- [ ] T2 · Bitácora y panel (internal/engine, internal/cli/watch.go)
  - R7. El evento `reject` sin gate se registra con `from`, `to=implementing` y `note`, sin `gate` (ya lo hace `entry`; prueba de engine que lo verifica).
  - R8. `describeEvent` con `Event == "reject"` y `Gate == ""` devuelve `"regresada" + note + arrow`; con gate, igual que hoy. Prueba en el test de watch existente.
  - R9. Prueba e2e en cmd/bflow/e2e_test.go: tarea light llevada a documenting, `bflow reject --to implementing --note x` sale con código 0 y deja la fase en implementing; sin `--note` sale con código 2.
- [ ] T3 · Docs y CHANGELOG
  - R10. docs/comandos.md: la fila de `reject` dice que en quality y documenting, sin gate pendiente, regresa la tarea a implementing.
  - R11. docs/como-probar-y-reportar.md: quitar la viñeta de #54.
  - R12. CHANGELOG.md, sección de la versión en curso, en «Agregado»: `bflow reject --note` regresa a implementing desde quality o documenting sin gate pendiente.
