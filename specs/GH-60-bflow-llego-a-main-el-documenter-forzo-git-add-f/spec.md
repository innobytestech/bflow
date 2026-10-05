# GH-60 · .bflow/ llegó a main: el documenter forzó git add -f

## Brief

**Objetivo:** que nada bajo `.bflow/` vuelva a entrar a una rama ni a un PR, y sacar lo que ya entró con GH-50.

**Entra**
- `guard`: nueva regla `bflow_tracked` que niega `git add`/`git stage` con `-f`/`--force` (cualquier ruta) y cualquier `git add` con una ruta bajo `.bflow/`. Al ser un deny de guard, queda en la fricción de `stats` como las demás reglas.
- `report DONE` (implementing y documenting) y `openPR` (aprobación que abre o actualiza el PR) rechazan con código `bflow_in_diff` si el diff contra la base agrega o modifica archivos bajo `.bflow/`; el motivo lista los archivos y dice cómo sacarlos (`git rm -r --cached <ruta>` + commit).
- Oficio del documenter: walkthrough y reportes se quedan en `.bflow/` y no se commitean; solo commitea comentarios y docs del repo; el changelog lo commitea bflow.
- `git rm -r --cached .bflow/tasks/GH-50/` en la rama de esta tarea.

**No entra**
- Revisar otros comandos que podrían colar archivos ignorados (`git update-index --add`, `git commit` con rutas ignoradas): no se han visto en la práctica.
- Cambiar `.gitignore` ni el lugar donde viven los artefactos.

**Decisiones nuevas**
- [N] El rechazo usa solo archivos agregados o modificados (`git diff --diff-filter=d`), no los borrados. Descartado: reusar `DiffNames`, que lista borrados y bloquearía justo el PR de esta tarea, que borra `.bflow/tasks/GH-50/` del índice.
- [N] Nuevo método `DiffKept` en `vcs.Git` en lugar de filtrar `DiffNames` comprobando el disco: los archivos de `.bflow/` siguen en disco aunque se saquen del índice, así que el disco no distingue.
- [N] La regla de guard aplica a toda sesión con hooks (principal y subagentes), no solo a subagentes: la sesión principal puede cometer el mismo error. Descartado: `a.Subagent` como condición.
- [N] Se niega `git add -f` sobre cualquier ruta, no solo `.bflow/`: con `-f` el único efecto extra es meter ignorados, y en este flujo nunca se quieren. Descartado: niega solo si alguna ruta cae bajo `.bflow/` (se cuela con globs o `-f .`).

**Riesgos**
- Las ramas abiertas antes del merge que ya tracen `.bflow/` desde su base no se bloquean (el diff de tres puntos no los muestra); se limpian solas al rebasar sobre main.
- Cambiar la interfaz `vcs.Git` obliga a tocar tres fakes de prueba.
- Tras editar `internal/agents/craft/documenter.md` hay que correr `bflow render` para regenerar `.claude/agents/bflow-documenter.md`.

**Tamaño:** S (guard + engine + adaptador git + oficio + limpieza; ~150 líneas con pruebas).

## Discovery

<!-- Carril light: sin discovery. -->

## Requirements

<!-- Carril light: los criterios van en Tasks. -->

## Design

<!-- Carril light: el diseño va en Brief y Tasks. -->

## Tasks

- [ ] T1 Guard: regla `bflow_tracked` en `internal/guard/guard.go`.
  - Regex nueva `addRe = ^git\s+(-\S+\s+)*(add|stage)\b(.*)$`. En `bash()`, por segmento: si los argumentos traen `--force` o un flag corto con `f` (`-f`, `-fA`, `-Af`) → `deny("bflow_tracked", ...)`; si algún argumento, normalizado con `\`→`/` y relativo a `c.Root` (`rel`), es `.bflow` o empieza con `.bflow/` → mismo deny. Sin condición de `Subagent` ni de fase.
  - Mensaje: "`<seg>` metería archivos que git ignora: lo de .bflow/ (walkthrough, reportes) se queda fuera del repo; bflow commitea la spec y el changelog. Agrega solo código y docs, sin -f."
  - Criterios: CUANDO un agente corre `git add -f .bflow/tasks/X/walkthrough.md`, `git add --force a.go`, `git add -fA`, `git -C . add .bflow/x` o `cd x && git add .bflow\x`, guard DEBE negar con regla `bflow_tracked`. CUANDO corre `git add internal/a.go`, `git add -A` o `git add -p`, DEBE permitir.
  - Pruebas: casos en la tabla de `internal/guard/guard_test.go` (allow y deny).
- [ ] T2 Git: `DiffKept(ctx, base string, paths []string) ([]string, error)` en `vcs.Git` (`internal/vcs/vcs.go`), implementado en `internal/adapters/vcs/git/git.go` como `git diff --name-only --diff-filter=d <base>...HEAD -- <paths>`. Fakes actualizados en `internal/check/check_test.go`, `internal/check/integration_test.go`, `internal/engine/vcs_test.go`.
  - Criterio: CUANDO la rama agrega `a.go`, modifica `b.go` y borra `c.go` respecto de la base, `DiffKept` DEBE devolver `a.go` y `b.go`, no `c.go`.
  - Prueba: `TestDiffKeptSkipsDeleted` en `internal/adapters/vcs/git/git_test.go` con repo real.
- [ ] T3 Engine: `func (e *Engine) bflowInDiff(ctx) []string` en `internal/engine/pr.go` (llama `DiffKept(e.diffBase(), []string{".bflow"})`, normaliza `\`→`/`; nil si no hay git, no hay base o falla) y `bflowInDiffRejection(files, when) *flow.Rejection` con `Code: "bflow_in_diff"`, la lista y "sácalos del índice con `git rm -r --cached <ruta>`, commitea y <when>".
  - Se llama en `engine.go` junto a cada `uncommitted` de DONE (implementing y documenting) y en `openPR` antes del push.
  - Criterios: CUANDO el diff contra la base agrega o modifica un archivo bajo `.bflow/`, `report DONE` DEBE rechazar con `bflow_in_diff` listándolo, y la aprobación que abre el PR DEBE rechazar sin hacer push ni abrir el PR. CUANDO el diff solo borra archivos de `.bflow/`, DEBE aceptar.
  - Pruebas en `internal/engine/vcs_test.go`: `TestDoneRejectsBflowInDiff`, `TestOpenPRRejectsBflowInDiff` (sin push en el fake), `TestBflowInDiffIgnoresDeleted`.
- [ ] T4 Oficio del documenter (`internal/agents/craft/documenter.md`): agregar que `walkthrough.md` y `reports/*` viven en `.bflow/tasks/<ID>/`, git los ignora y no se agregan ni con `-f`; reescribir la última viñeta para que diga que se commitean solo los comentarios y docs del repo que cambió (el changelog lo commitea bflow). Correr `bflow render` para regenerar `.claude/agents/bflow-documenter.md`. Si hay prueba de oficio con golden, actualizarla.
- [ ] T5 Limpieza: `git rm -r --cached .bflow/tasks/GH-50/` y commit en la rama. Criterio: `git ls-files .bflow` vacío al final de la rama, y `report DONE` de esta tarea pasa (el borrado no dispara T3).
