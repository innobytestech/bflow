# GH-55 · El documenter omite CHANGELOG cuando cambia la salida pública

## Brief

**Objetivo:** que una tarea cuyo diff toca salida pública no pase documenting sin changelog para consumidores, salvo que una persona lo exima explícitamente.

**Entra**
- Config nueva `flow.public_paths`: lista de rutas del repo cuya modificación cambia lo que ve quien usa el producto (comandos, JSON, panel, config). Una entrada que termina en `/` cubre la carpeta; las demás se comparan con `path.Match`; una entrada con `!` al inicio excluye (p. ej. `!*_test.go`; un patrón sin `/` se compara también contra el nombre base).
- Gate en `report DONE` del documenter: si el diff contra la base toca `public_paths` y el changelog para consumidores (`ChangelogPath`) no cambió (ni en commits ni sin commitear), bflow rechaza con código `changelog_required` y lista las rutas públicas tocadas.
- Exención humana: `bflow unblock <id> --no-changelog --note "<motivo>"` (unblock ya es solo de personas en el guard). El documenter, si cree que no hace falta changelog, reporta BLOCKED con su motivo; la persona decide.
- Oficio del documenter (`internal/agents/craft/documenter.md` y los dos renderizados): changelog obligatorio si el diff toca salida pública (comandos, JSON, panel, config) o `public_paths`; para omitirlo, BLOCKED, nunca "sin cambio" en `reports/docs.md`.
- `bflow.yaml` de este repo declara sus rutas públicas; docs (`configuracion.md`, `guia.md`, `comandos.md`, `CHANGELOG.md`) y se quita #55 de `como-probar-y-reportar.md`.

**No entra**
- Aviso en `bflow check`: el changelog se escribe en documenting, después del check; avisar en implementing sería ruido.
- Juzgar si el contenido del changelog es suficiente: solo se exige que cambie.
- Rutas públicas por defecto: sin `public_paths` el gate no actúa (bflow no sabe qué es público en otro repo).

**Decisiones nuevas**
- [N] Gate duro en `report DONE` de documenting (como `docs_pending`). Descartado: solo aviso en `check`/`report`, que es lo que GH-46 ya ignoró.
- [N] Lista configurable `flow.public_paths`. Descartado: globs fijos en el binario (bflow es genérico).
- [N] Exención con `unblock --no-changelog`, que queda en la bitácora con quién y el motivo. Descartado: línea "sin cambio" en `reports/docs.md`, porque la escribe el agente, no la persona. Descartado: `approve --gate`, porque documenting no tiene gate.
- [N] "Changelog cambió" = la ruta de `ChangelogPath` está en `DiffNames(base)` o en `Dirty`. bflow lo commitea después, así que al reportar suele estar sin commit.

**Riesgos**
- `public_paths` demasiado amplias (p. ej. `internal/cli/`) exigen changelog en refactors internos; la exención cubre esos casos y se puede afinar con `!`.
- Sin base (`diffBase` vacío) o sin git, el gate no actúa, igual que `docs_pending`.

**Tamaño:** S-M. Config + función pura + gate + flag + oficio + docs; ~6 archivos de código y sus pruebas.

## Discovery

No aplica (carril light).

## Requirements

En carril light los criterios van dentro de Tasks.

## Design

- `config.Flow.PublicPaths []string` (`yaml:"public_paths,omitempty"`) se copia a `flow.Config.PublicPaths`. Validación: ruta del repo, no absoluta, sin `..`.
- `review.PublicTouched(diff, patterns []string) []string` (pura, en `internal/review/review.go`): rutas del diff (normalizadas a `/`) que caen en una entrada positiva y en ninguna `!`. Entrada con `/` final = prefijo; si no, `path.Match` contra la ruta completa y, si el patrón no tiene `/`, contra el nombre base.
- `(*Engine).changelogRequired(ctx, rec) *flow.Rejection` en `internal/engine/review.go`, llamada en `engine.go` justo después de `docsPending` en el DONE de documenting. Devuelve nil si: no hay `PublicPaths`, no hay git o base, `PublicTouched` vacío, `ChangelogPath` está en `DiffNames` o en `Dirty([cl])`, o hay exención.
- Exención: `flow.Event.NoChangelog bool` (`json:"no_changelog,omitempty"`); `Engine.Unblock(ctx, id, note string, noChangelog bool)`; la entrada `unblock` de la bitácora lleva `data.no_changelog: true`. Vale la última entrada `unblock` con esa marca posterior a la última entrada a documenting. `--no-changelog` exige `--note` (error `note_required` en CLI).
- Rechazo `changelog_required` (en español): lista de rutas públicas tocadas, la ruta del changelog a escribir, y "si no hace falta, reporta BLOCKED con el motivo; una persona puede eximirlo con `bflow unblock <id> --no-changelog --note`".
- Seguridad: la exención solo pasa por `unblock`, que está en `guard.HumanOnly`; un agente no puede eximirse. Sin datos sensibles.

## Tasks

- [ ] T1. Config: `flow.public_paths` en `config.Flow` y `flow.Config`, con validación. Prueba `TestPublicPathsValidated` (`internal/config/config_test.go`): rechaza absoluta, `..` y `.bflow/`; acepta `internal/cli/` y `!*_test.go`.
- [ ] T2. `review.PublicTouched`. Prueba `TestPublicTouched`: prefijo de carpeta, `path.Match`, nombre base, exclusión con `!`, separadores `\`. Criterio: CUANDO una ruta cae en una entrada positiva y en una `!`, NO DEBE contar como pública.
- [ ] T3. Gate `changelog_required` en el DONE de documenting. Pruebas en `internal/engine/review_test.go`:
  - `TestChangelogRequiredRejects`: diff toca ruta pública y el changelog no cambió → rechazo `changelog_required` con la ruta tocada.
  - `TestChangelogRequiredPassesWithChangelog`: el changelog está en el diff, o sin commitear → acepta.
  - `TestChangelogRequiredIgnoredWithoutPublicPaths`: sin `public_paths` o sin ruta pública tocada → acepta.
  Criterio: CUANDO el documenter reporta DONE y el diff toca `public_paths` sin cambio en el changelog ni exención, el sistema DEBE rechazar con `changelog_required`.
- [ ] T4. Exención: flag `--no-changelog` en `unblock` (exige `--note`), `Event.NoChangelog`, marca en la bitácora. Pruebas: `TestUnblockNoChangelogExempts` (engine: tras BLOCKED en documenting y `unblock --no-changelog`, el DONE pasa sin changelog; una exención de una entrada anterior a documenting no vale) y `TestUnblockNoChangelogNeedsNote` (cli). Criterio: CUANDO una persona corre `unblock --no-changelog --note`, el sistema DEBE aceptar el siguiente DONE de documenting sin changelog y dejar el motivo en la bitácora.
- [ ] T5. Oficio: `internal/agents/craft/documenter.md` dice que el changelog es obligatorio si el diff toca salida pública (comandos, salida JSON, panel, config) o `flow.public_paths`, y que para omitirlo se reporta BLOCKED con el motivo, no "sin cambio" en `reports/docs.md`. `bflow render` regenera `.claude/agents/bflow-documenter.md` y `.opencode/agents/bflow-documenter.md`. Prueba `TestDocumenterChangelogPublicOutput` (`internal/agents/agents_test.go`) comprueba esas frases en el oficio renderizado.
- [ ] T6. Repo y docs: `bflow.yaml` con `public_paths` (`cmd/`, `internal/cli/`, `internal/config/config.go`, `internal/agents/`, `!*_test.go`); `docs/configuracion.md` (clave nueva), `docs/comandos.md` (`unblock --no-changelog`), `docs/guia.md` l.80, entrada en `CHANGELOG.md`, y quitar #55 de `docs/como-probar-y-reportar.md`. `bflow render --check` y `bflow check` en verde.
