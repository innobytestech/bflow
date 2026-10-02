# Walkthrough GH-50

El cambio agrega un bloqueo en `bflow report --agent reviewer --verdict APPROVED` cuando quedan archivos del diff sin abrir, además de los que ya se marcaban en el review-map (🔴). El objetivo es que el reviewer no pueda aprobar sin leer todo el código y las pruebas.

Comienza en **`internal/review/review.go`**: tres nuevas funciones definen qué se exige y qué queda exento. `IsGenerated` detecta lockfiles (go.sum, package-lock.json, etc.) por nombre, `Exempt` combina eso con docs y `specs/`, y `Measure` ahora calcula un campo `Pending` con los archivos obligatorios que no se abrieron, excluyendo los de `RedMissing` (para no duplicar los 🔴 ya conocidos). Las pruebas `TestIsGenerated`, `TestExempt` y `TestMeasurePending` cubren R4 (exentos) y R1/R3/R5/R6 (cálculo de pendientes).

Continúa en **`internal/adapters/vcs/git/git.go`**: nuevo método `DiffBinaries` que usa `git diff --numstat` para detectar binarios (retorna rutas con `-` en ambas columnas). Así el engine sabe qué no exigir sin guardar una lista de extensiones. Si git falla, la cobertura se mide sin exentarlos (R11).

El **engine** (`internal/engine/review.go`) llama a `DiffBinaries`, pasa los binarios a `Measure`, y la función `reviewIncomplete` ahora rechaza APPROVED si `cov.Pending` no está vacío. El rechazo lista hasta 20 pendientes (con `ListCapped`, exportado desde review.go) más un "y K más" si hay más, y sugiere `git diff <base>...HEAD -- <ruta>` o un `git diff` completo si el diff mide menos de 1,500 líneas (R2). La prueba `TestReviewerApprovedNeedsAllCode` verifica que GH-46 (🔴 leídos pero pruebas sin abrir) rechaza.

Las **instrucciones actualizadas** en `internal/agents/craft/reviewer.md` (línea 5) y regeneradas en `.claude/agents/bflow-reviewer.md` dejan claro que "bflow exige abrir todo el código y las pruebas del diff" (no docs, `specs/`, lockfiles ni binarios), con la forma de hacerlo en diffs chicos. `docs/guia.md:34` describe la medición en el contexto del walkthrough, y `CHANGELOG.md` resume la regla y el campo `pending` nuevo.

Con REJECTED nunca se bloquea (R9), y sin cobertura medida el reporte pasa sin cambios (R8). Las pruebas congeladas de GH-20 se ajustaron para que lean los archivos faltantes o pasen a REJECTED donde se verifica que se registre el `pending` correcto.
