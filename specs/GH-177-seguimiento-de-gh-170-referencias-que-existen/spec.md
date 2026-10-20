# GH-177 · Seguimiento de GH-170: referencias que existen en el worktree, corrida huérfana y spec

## Brief

Objetivo: que el eval del reviewer cuente una cita `ruta:N` solo si la ruta es un archivo del worktree del caso, y cerrar dos cabos sueltos de GH-170 (prueba de la corrida huérfana y texto de su spec).

Entra:
- `internal/eval/score.go`: `Findings` descarta las citas cuya ruta no es un archivo regular dentro del worktree del caso.
- La lista `extensionless` desaparece: un nombre sin extensión ni separador (`Brewfile:3`, `Makefile:3`) cuenta si existe y se descarta si no.
- `Runner.Report` rechaza con `worktree_missing` si el worktree del caso ya no está en disco, para no puntuar cero en silencio.
- `TestOrphanRunDirIsNotOpen` comprueba además que el siguiente `Next` arranca una corrida nueva.
- La spec de GH-170 (T1) describe la prueba como es: "alfa" primero, "zeta" después, espera "zeta".

No entra: cambiar la tolerancia, el cálculo de recall/falsos positivos, las exclusiones por línea o encabezado, ni validar que la línea N exista dentro del archivo.

Decisiones nuevas:
- [N] La existencia en el worktree reemplaza a la lista `extensionless`. Descartado: conservar la lista y validar además, porque deja fuera los nombres que no están en ella (`Brewfile`) y la lista solo sirve para filtrar lo que la existencia ya filtra.
- [N] Se exige archivo regular (`os.Stat` sin error y `Mode().IsRegular()`); un directorio no cuenta. Descartado: aceptar cualquier entrada, porque `internal:3` contaría.
- [N] Se valida contra el worktree en disco al reportar (ya está en el commit head del caso y aún no se limpió: `Findings` corre en `run.go:372`, la limpieza después). Descartado: consultar git por commit, que agrega llamadas sin cambiar el resultado.
- [N] Worktree ausente al reportar es un rechazo (`worktree_missing`), no un resultado con recall 0. Descartado: puntuar igual, que esconde el problema.

Riesgos:
- Las pruebas de `score_test.go`, `score_norm_test.go` y `run_test.go` usan worktrees falsos (`/wt`) o vacíos; hay que darles archivos reales. Es la mayor parte del trabajo.
- La expresión se vuelve más permisiva (cualquier nombre antes de `:N`); `localhost:8080`, `paso:2` o `10:30` dejan de filtrarse por forma y pasan a filtrarse por existencia. Una prueba lo fija.
- En Windows `os.Stat` no distingue mayúsculas; una cita `Internal/A.go:3` cuenta como hallazgo pero con la ruta tal como se citó, y no casa con el defecto. Igual que hoy; no se toca.

Tamaño: S (un archivo de producción, pruebas, una línea de spec).

## Discovery

<!-- Discovery completo; lo escribe bflow al cerrar el gate discovery. No lo edites. -->

## Requirements

<!-- Criterios EARS numerados R1..Rn, cada uno [D] (del discovery) o [N] (nuevo). -->

## Design

<!-- Estructuras, firmas, decisiones tomadas y descartadas, superficie de seguridad. -->

## Tasks

- [ ] T1 Una cita cuenta solo si su ruta es un archivo del worktree.
  - En `internal/eval/score.go`: borrar `extensionless` y `extensionlessAlt`; la alternativa de `refRe` para nombres sin extensión ni separador pasa a `[\w.-]+` (comentario actualizado: la forma ya no filtra, filtra la existencia). En `findIn`, tras `relToWorktree`, descartar la cita si `os.Stat(filepath.Join(worktree, filepath.FromSlash(file)))` falla o no es `Mode().IsRegular()`. Con `worktree == ""` no hay hallazgos.
  - Criterios:
    - CUANDO una cita `ruta:N` o `ruta:N-M` nombra un archivo regular del worktree, el sistema DEBE contarla como hoy (relativa, con "/").
    - CUANDO la ruta no existe en el worktree (`2026/10:3`, `internal/zzz.go:5` sin archivo), el sistema NO DEBE contarla.
    - CUANDO la ruta es un directorio del worktree, el sistema NO DEBE contarla.
    - CUANDO un nombre sin extensión ni separador existe (`Brewfile:3`, `Makefile:3`), el sistema DEBE contarlo; CUANDO no existe (`localhost:8080`, `paso:2`, `MiMakefile:3`), NO DEBE.
    - Las exclusiones (`excludedLine`, `excludedHeading`) y el rechazo de rutas fuera del worktree DEBEN seguir igual.
  - Pruebas: helper `worktreeWith(t, files ...string) string` en `score_test.go` (crea los archivos en `t.TempDir()`); las pruebas existentes de `Findings` pasan a usarlo con los archivos que esperan atrapar. `TestFindingsExtensionlessPaths` se reescribe: con `Makefile`, `Dockerfile`, `Brewfile` y `cmd/tool` creados cuentan; `localhost:8080`, `paso:2`, `MiMakefile:3` no. Nuevas: `TestFindingsSkipsMissingFiles` (`2026/10:3` y `internal/nada.go:4` no cuentan) y `TestFindingsSkipsDirectories` (`internal:3` con `internal/` creado no cuenta). `TestFindingsTrimsWorktreePrefix` conserva sus casos de Windows y mayúsculas con un worktree real.
- [ ] T2 Report rechaza si el worktree del caso no está.
  - En `Runner.Report` (`internal/eval/run.go`), antes de leer `out`: si `os.Stat(wt)` falla, `&Rejection{"worktree_missing", "falta el worktree <wt>: aborta la corrida con --abort y empieza otra"}`. El caso sigue en curso.
  - Criterio: CUANDO se reporta un caso cuyo worktree ya no existe, el sistema DEBE rechazar con `worktree_missing` sin guardar resultado.
  - Pruebas: `TestReportRejectsMissingWorktree` en `run_test.go`. Las pruebas de `run_test.go` que esperan `Caught` o falsos positivos (`TestReportScoresAndRemovesWorktree`, `TestReportKeepsResultWhenRemoveFails` y las de totales) crean en el worktree los archivos que citan; el falso positivo `internal/zzz.go:5` y la nota `internal/q.go:1` necesitan su archivo para seguir contando.
- [ ] T3 La corrida huérfana no se retoma.
  - En `TestOrphanRunDirIsNotOpen` (`internal/eval/run_test.go`): guardar `first := h.r.Open()` antes de abortar; tras las comprobaciones actuales, adelantar `h.now` un minuto, llamar `h.next(Opts{})` y exigir `Spawn != nil`, `Spawn.Args["id"] == "alfa"` y `h.r.Open()` no vacío y distinto de `first`.
  - Criterio: CUANDO `cleanup` dejó la carpeta sin `run.json`, el siguiente `Next` DEBE empezar una corrida nueva desde el primer caso.
- [ ] T4 Texto de la spec de GH-170.
  - En `specs/GH-170-seguimiento-del-eval-del-reviewer-gh-152/spec.md`, T1, línea de "Prueba": dos casos; las llamadas usan "alfa" (t0+10s) y luego "zeta" (t0+70s); espera "zeta", el más reciente, que el orden alfabético no habría elegido.
  - Criterio: el texto DEBE coincidir con `TestFinishModelTieUsesLatest` en modelos, orden y resultado esperado.
