# GH-133 · Se está creando un discovery file.

## Brief

**Objetivo:** que el discovery completo viva en un solo lugar del repo, la sección `## Discovery` de `spec.md`, y deje de aparecer como `discovery.md` suelto.

Hoy el mismo texto queda en hasta cuatro lugares: el archivo que la sesión principal escribe para pasarlo con `--file` (en la captura del issue aparece junto a `spec.md`), la copia `.bflow/tasks/<ID>/discovery.md` que guarda `Approve` (`internal/engine/commands.go:93`), el comentario `## Discovery` del tracker (`internal/flow/machine.go:195`) y el resumen de la sección `## Discovery` de la spec.

**Entra**
- `approve --gate discovery` acepta `--stdin`: el discovery llega por stdin y no hace falta ningún archivo. `--file` sigue funcionando.
- La opción del gate discovery (`internal/flow/next.go:147`) y la skill `## discovery` del líder (`adapters/leader/bflow.md:24`) usan `--stdin` y ya no piden escribir un archivo.
- Al cerrar el gate, bflow escribe el discovery completo en `## Discovery` de `spec.md` y ya no crea `.bflow/tasks/<ID>/discovery.md`.
- `bflow show <ID> discovery` lee esa sección. Si está vacía, lee el `discovery.md` viejo, para las tareas que ya lo tienen.
- Cambia la pista de la sección en la plantilla (`internal/engine/spec.go:27`). La actualizan los docs (`docs/conceptos.md:104-106`, `docs/comandos.md`), el CHANGELOG y las skills de los adaptadores.

**No entra**
- Quitar el comentario del tracker: se queda, porque es donde la persona lee el discovery sin abrir el repo (`docs/conceptos.md:106`).
- Borrar o migrar los `discovery.md` que ya están en `.bflow/tasks/`.
- El carril light: no tiene fase discovery (`internal/flow/config.go:72`).

**Decisiones nuevas**
- [N] El repo guarda el discovery dentro de la spec, que bflow llena al cerrar el gate. Descartada: dejarlo solo en el tracker. El spec-author tendría que leerlo de la red en cada corrida, y `show discovery` dependería del tracker.
- [N] Entra por stdin, igual que el reporte del scout (límite de 64 KiB, código `discovery_too_long`). Descartada: que el líder lo escriba en la spec con Edit. Eso pasa antes de que la spec exista y deja el formato a cargo del agente.
- [N] El spec-author no escribe la sección `## Discovery`. La lee con `show discovery` y la deja como está.

**Riesgos**
- La spec del PR se alarga con el discovery completo. Se acepta porque es lo que pide el issue.
- Un `--file` que apunte a un archivo dentro de `specs/` todavía puede dejar el archivo suelto. El líder ya no lo usa.

**Tamaño:** S. Son unos 6 archivos Go y sus pruebas, más docs y skills.

## Discovery

<!-- Resumen en ≤5 líneas. El discovery completo está como comentario en el tracker. -->

## Requirements

<!-- Criterios EARS numerados R1..Rn, cada uno [D] (del discovery) o [N] (nuevo). -->

## Design

<!-- Estructuras, firmas, decisiones tomadas y descartadas, superficie de seguridad. -->

## Tasks

- [x] T1 `approve` lee el discovery por stdin. En `internal/cli/flowcmds.go` (comando `approve`) se agrega `fs.Bool("stdin", ...)`.
  - CUANDO llega `--stdin`, el sistema DEBE leer hasta 64 KiB de `c.Stdin` y guardarlo en `ApproveOpts.Attachment`. Si pasa de 64 KiB, DEBE rechazar con `discovery_too_long`.
  - CUANDO llegan `--stdin` y `--file` juntos, el sistema DEBE rechazar con `usage`.
  - `--stdin` solo aplica al gate discovery. Con otro gate, DEBE rechazar con `bad_attachment`.
  - Pruebas: `TestApproveDiscoveryStdin` y `TestApproveDiscoveryStdinTooLong` en `internal/cli`.
- [x] T2 La opción y la skill dejan de pedir un archivo.
  - En `gateNext` de `internal/flow/next.go`, el comando de la opción `approve` DEBE ser `bflow approve <id> --gate discovery --stdin`, y la pregunta ya no menciona ningún archivo.
  - En `adapters/leader/bflow.md`, `## discovery` DEBE decir: "Al cerrar, pasa el discovery completo por stdin al comando de la opción; no lo guardes en un archivo."
  - Se actualizan `internal/flow/next_test.go` y las pruebas de adaptadores que fijan el texto.
- [x] T3 El discovery queda en la spec y no en `.bflow`.
  - En `Engine.Approve` (`internal/engine/commands.go:93`), CUANDO se cierra el gate discovery con adjunto, el sistema DEBE dejar de escribir `discovery.md` y DEBE reemplazar el cuerpo de `## Discovery` de `spec.md` con el adjunto (sin espacios sobrantes). El tracker sigue recibiendo su comentario.
  - Para eso se agrega `func (e *Engine) fillSection(rec store.Record, key, body string) error` en `internal/engine/spec.go`. Reemplaza el cuerpo entre `## <Heading>` y el siguiente `## `, escribe con `store.WriteAtomic` y crea la spec con `ensureSpec` si todavía no existe.
  - Si falla, va un aviso en `out.Warnings` ("no se pudo escribir el discovery en la spec: ..."), igual que hoy.
  - La pista de la sección pasa a ser: "Discovery completo; lo escribe bflow al cerrar el gate discovery. No lo edites."
  - Pruebas: se amplía `internal/engine/engine_test.go:108`. Comprueba que `## Discovery` trae "entra: X" y que `.bflow/tasks/<id>/discovery.md` no existe. El e2e (`cmd/bflow/e2e_test.go:195`) pasa a usar `--stdin`.
- [x] T4 `show discovery` lee la spec.
  - En `Engine.Show`, CUANDO se pide `discovery`, el sistema DEBE devolver la sección `discovery` de la spec. CUANDO esa sección está vacía o solo trae la pista, DEBE devolver `.bflow/tasks/<id>/discovery.md` si existe. Si tampoco existe, DEBE devolver el error de siempre ("todavía no tiene discovery").
  - `"discovery"` sale del mapa `artifacts`.
  - Pruebas: `TestShowDiscoveryFromSpec` y `TestShowDiscoveryLegacyFile` en `internal/engine`.
- [x] T5 Oficio, docs y changelog.
  - `internal/agents/craft/spec-author.md` DEBE decir que la sección `## Discovery` la llena bflow, que el spec-author no la toca y que la lee con `bflow show <id> discovery`.
  - Se corre `bflow render` para regenerar `.claude/agents/`.
  - `docs/conceptos.md:104-106` DEBE decir que el discovery completo está en la spec y en el tracker. `docs/comandos.md` documenta `approve --stdin`.
  - Una entrada en `CHANGELOG.md`.
  - `bflow check` pasa.
