# GH-50 · El reviewer sigue aprobando sin leer el diff (seguimiento de #20)

## Brief

**Objetivo:** que `bflow report --agent reviewer --verdict APPROVED` se rechace mientras quede sin abrir código o pruebas del diff, y que el rechazo diga cuáles faltan.

**Por qué GH-20 no bastó:** la medición sí funciona (los 8 `review_coverage` desde GH-20 tienen `measured:true`). El problema es que solo bloquea por los 🔴, y los 🔴 los elige el propio reviewer: marca lo que ya leyó y aprueba. `cov.Unread` se calcula, pero nunca bloquea. Ejemplos: GH-46 leyó 4 de 9 archivos (no abrió ninguna prueba) y GH-8, 7 de 34 (dejó 12 de código sin abrir). Los dos se aprobaron.

**Entra:**
- APPROVED exige abrir todo archivo del diff que no esté exento, además de los 🔴 (regla actual). El rechazo `review_incomplete` nombra hasta 20 pendientes, cierra con "y K más" y sugiere `git diff <base>...HEAD -- <ruta>` o Read.
- Las pruebas son obligatorias. Quedan exentos los docs (`review.IsDoc`), `specs/`, los lockfiles conocidos y los binarios.
- Un archivo borrado en la rama se cubre con `git diff|show ... -- <ruta>`, aunque ya no exista en disco.
- Instrucciones del reviewer (`internal/agents/craft/reviewer.md`, regenerado con `bflow render`), `docs/guia.md` y CHANGELOG.

**No entra:**
- Sin medición (sin hook de lecturas, como en OpenCode, o si git falla) el reporte pasa como hoy.
- No cambian el hook (no bloquea al agente), `isReviewer` del hook ni el display o stats, más allá del campo nuevo.
- No hay configuración nueva para los exentos.

**Diffs chicos:** no se encarecen. Un `git diff <base>...HEAD` sin ruta, con menos de 1,500 líneas, sigue cubriendo todo: con 1 o 2 archivos basta una llamada.

**Decisiones nuevas:**
- [N] Los binarios se detectan con `git diff --numstat` (`-` en las dos columnas) mediante un método nuevo, `vcs.Git.DiffBinaries`. Se descarta una lista de extensiones: no detecta binarios sin extensión y marca de más.
- [N] Los pendientes van en un campo nuevo, `Coverage.Pending` (`json:"pending,omitempty"`). `Unread` y `UnreadOther` conservan su significado, así que los eventos viejos y stats siguen igual. Se descarta redefinir `Unread`, porque rompería la lectura de los eventos ya registrados.
- [N] Después de `--`, una ruta de `git diff|show` se registra sin comprobar que exista, porque ahí git siempre la toma como pathspec (*pathspec*: argumento que git interpreta como ruta, no como ref). Antes del `--` se sigue exigiendo que exista, para no confundir una ref con una ruta.
- [N] Lockfiles (lista fija, por nombre base): `go.sum`, `go.work.sum`, `package-lock.json`, `npm-shrinkwrap.json`, `pnpm-lock.yaml`, `bun.lockb` y cualquier `*.lock`.

**Riesgos:**
- Revisar diffs grandes (1,500 líneas o más) cuesta más tokens: es el objetivo.
- Un Read fallido sobre un archivo borrado también cuenta como leído. Ya pasaba así; se acepta.
- Hay que ajustar 3 pruebas de GH-20 que aprobaban con `internal/c.go` sin abrir.

**Tamaño:** M. Son unas 5 tareas: lógica pura en `internal/review`, una llamada a git en el adaptador, el engine, las instrucciones y la documentación.

## Discovery

- La medición de GH-20 funciona, pero solo bloquea por los 🔴, que elige el mismo reviewer. Por eso aprueba sin abrir código ni pruebas (GH-46: 4 de 9; GH-8: 7 de 34).
- Decidido: exigir todo el diff salvo docs, `specs/`, lockfiles y binarios, y que las pruebas cuenten. Sin medición, pasa. En diff chico basta un `git diff` completo. Los borrados se cubren con `git diff -- <ruta>`.
- La lógica pura va en `internal/review`; git y disco, en el engine. El rechazo ocurre solo en `report`.

## Requirements

- R1 [D] CUANDO el reviewer reporte APPROVED en quality con la cobertura medida y quede sin abrir algún archivo del diff que no esté exento, el sistema DEBE rechazar con `review_incomplete` y no avanzar de fase.
- R2 [D] El rechazo de R1 DEBE listar los archivos pendientes en el orden del diff, a lo sumo 20, seguidos de "y K más" si hay más. También DEBE sugerir `git diff <base>...HEAD -- <ruta>` o Read y, si el diff tiene menos de 1,500 líneas, un `git diff <base>...HEAD` completo. Si no hay base configurada, el texto dice `<base>`.
- R3 [D] Los archivos de prueba (según `guard.test_patterns`) DEBEN contar como obligatorios.
- R4 [D] Son exentos y NO DEBEN aparecer como pendientes: los docs (`review.IsDoc`), las rutas bajo `specs/`, los lockfiles de la lista fija y los binarios que reporta git.
- R5 [N] Un archivo que quede pendiente por R1 y además esté en `RedMissing` DEBE aparecer una sola vez en el rechazo, en el bloque de 🔴, que no cambia.
- R6 [D] CUANDO haya una lectura `Whole` (un `git diff` sin ruta) y el diff tenga menos de `WholeDiffMax` líneas, todos los archivos DEBEN quedar cubiertos. Con 1500 líneas o más, NO.
- R7 [D] CUANDO el reviewer corra `git diff` o `git show` con rutas después de `--`, el sistema DEBE registrarlas como leídas aunque no existan en disco (archivos borrados en la rama).
- R8 [D] CUANDO la cobertura no esté medida (no hay lecturas del hook en la ventana, git falla o no hay review-map), el sistema NO DEBE rechazar por R1.
- R9 [D] REJECTED NO DEBE bloquearse por pendientes y DEBE seguir registrando `review_coverage`.
- R10 [N] `review_coverage` DEBE guardar los pendientes en `pending` (omitido si está vacío). Los eventos viejos, sin `pending`, DEBEN leerse igual que hoy en display y stats.
- R11 [N] `vcs.Git.DiffBinaries(ctx, base)` DEBE devolver los archivos binarios del diff de tres puntos contra la base (numstat `-\t-`). Si falla, la cobertura se mide sin exentar binarios (no se deja de medir).
- R12 [D] Las instrucciones del reviewer DEBEN decir que bflow exige abrir todo el código y las pruebas del diff (no los docs, `specs/`, lockfiles ni binarios), que con menos de 1,500 líneas basta un `git diff <base>...HEAD` completo, y que con APPROVED y un pendiente bflow rechaza. `.claude/agents/bflow-reviewer.md` DEBE quedar regenerado (`bflow render --check` en verde).
- R13 [N] `docs/guia.md` (párrafo de cobertura del walkthrough) y `CHANGELOG.md` DEBEN describir la regla nueva y el campo `pending`.

## Design

### internal/review (puro)

```go
// Coverage: campos nuevos
Pending   []string `json:"pending,omitempty"` // no leídos, obligatorios (código y pruebas, no exentos), en orden del diff
DiffLines int      `json:"-"`                 // líneas del diff medido; solo para el texto del rechazo

// IsGenerated dice si p es un lockfile conocido: base go.sum, go.work.sum,
// package-lock.json, npm-shrinkwrap.json, pnpm-lock.yaml, bun.lockb, o extensión .lock.
func IsGenerated(p string) bool

// Exempt dice si p no se exige al reviewer: IsDoc, bajo specs/ o IsGenerated.
func Exempt(p string) bool

// Measure: nuevo parámetro binary (rutas binarias del diff; puede ser nil).
func Measure(diff []string, diffLines int, reads []Read, red []string, isTest func(string) bool, binary []string) Coverage
```

- `Measure` llena `Unread` y `UnreadOther` igual que hoy. Además, un archivo no cubierto que no sea `Exempt` ni esté en `binary` va a `Pending`, salvo que ya esté en `RedMissing` (R5). Como `RedMissing` se calcula después, primero se arma `Pending` completo y luego se le quitan los de `RedMissing`.
- `Lines()` no cambia: tras APPROVED, `Pending` siempre queda vacío, y en REJECTED la nota del reviewer ya explica.
- `fromGit`: lo que va después de `--` se agrega con `add` (normaliza y descarta lo que cae fuera del repo), sin `exists` (R7). Lo que va antes del `--` sigue igual.
- Se mueve la función `list` (tope de 20) o se exporta como `ListCapped(ps []string) string` para que el engine la use en el rechazo (R2).

### internal/vcs y adaptador git

- `vcs.Git` suma `DiffBinaries(ctx context.Context, base string) ([]string, error)`. El adaptador corre `git diff --numstat --no-renames <base>...HEAD` y devuelve las rutas con `-` en las dos primeras columnas. `fakeGit` (`internal/engine/vcs_test.go`) suma el campo `binary []string`.

### internal/engine

- `reviewCoverage` llama a `DiffBinaries`. Si falla, sigue con `nil` (R11). Luego pasa las rutas, normalizadas con `/`, a `Measure`.
- `reviewIncomplete(cov, m, hasMap, base string)`: si `base == ""`, el texto usa `<base>`. Con `cov.Measured && len(cov.Pending) > 0` agrega este bloque, después del de 🔴:
  `Archivos del diff (código y pruebas) que no abriste:\n- <ListCapped, uno por línea>\nÁbrelos con Read o ` + "`git diff <base>...HEAD -- <ruta>`" + `[; con menos de 1,500 líneas basta un ` + "`git diff <base>...HEAD`" + ` completo].`
  Para la lista con tope y un archivo por línea se usan los primeros 20 y luego la línea `- y K más`.
- `engine.go:147-156` no cambia, salvo que ahora pasa `e.diffBase()`.

### Instrucciones y docs

- `internal/agents/craft/reviewer.md`, línea 5: se reemplaza por la regla de R12. Después, `bflow render` regenera `.claude/agents/bflow-reviewer.md`.
- `docs/guia.md:34`: con APPROVED, también rechaza si queda sin abrir código o pruebas del diff (salvo docs, `specs/`, lockfiles y binarios).
- `CHANGELOG.md`, v0.1.0, sección Agregado (o Cambiado, si existe): una entrada con la regla, los exentos y `pending` en `data.stats.review`.

### Decisiones descartadas

- Detectar binarios por extensión: no ve los binarios sin extensión y marca de más.
- Redefinir `Unread` para incluir las pruebas: rompe la lectura de los eventos ya registrados.
- Bloquear desde el hook: va contra R5 de GH-20, porque el rechazo es solo en `report`.
- Exigir solo un mínimo (por ejemplo, un porcentaje): el reviewer elegiría otra vez qué abrir.

### Superficie de seguridad

- Las rutas que se registran después de `--` pasan por `Normalize`: lo que cae fuera del repo se descarta. Nada se ejecuta ni se lee con esas rutas, solo se comparan.
- `DiffBinaries` usa el mismo `run` de git que `DiffLines` (sin shell), con la base de la configuración.
- Un error de git no bloquea a nadie: la cobertura queda sin medir (`DiffNames`/`DiffLines`) o sin exentar binarios (`DiffBinaries`).
- Errores: el rechazo solo nombra rutas del diff. No expone datos sensibles.

## Tasks

- [ ] T1 Contrato. Firmas y pruebas nuevas que fallan.
  - Firmas: `review.IsGenerated`, `review.Exempt`, `review.ListCapped`, `Coverage.Pending`/`DiffLines`, `Measure(..., binary []string)`, `vcs.Git.DiffBinaries` y `fakeGit.binary`. Las llamadas existentes a `Measure` pasan `nil`.
  - Pruebas en `internal/review/review_test.go`: `TestIsGenerated` (R4), `TestExempt` (R4), `TestMeasurePending` (R1, R3, R4, R5, R6) y `TestFromActionGitDeleted` (R7).
  - Prueba en `internal/adapters/vcs/git/git_test.go`: `TestDiffBinaries` (R11).
  - Pruebas en `internal/engine/review_test.go`: `TestReviewerApprovedNeedsAllCode` (R1, R3; caso GH-46: 🔴 leídos, una prueba sin abrir), `TestReviewerPendingListCapped` (R2; 25 pendientes, 20 y "y 5 más", sugerencia con base), `TestReviewerExemptNotRequired` (R4; docs, `specs/`, `go.sum` y binario sin abrir pasan), `TestReviewerSmallDiffWholeCovers` (R6; 2 archivos y un `git diff` entero pasan), `TestReviewerPendingNotOnRejected` (R9, R10; REJECTED pasa y guarda `pending`) y `TestReviewerPendingUnmeasured` (R8).
- [ ] T2 `internal/review`: `IsGenerated`, `Exempt`, `ListCapped`, `Pending` en `Measure` y rutas después de `--` sin `exists` en `fromGit`. (R3-R7, R10)
- [ ] T3 `vcs.Git.DiffBinaries` en la interfaz y el adaptador git. (R11)
- [ ] T4 Engine: `reviewCoverage` con binarios y `reviewIncomplete` con el bloque de pendientes y la base. Hay que ajustar `TestReviewerApprovedNeedsRedRead`, `TestReviewCoverageLogged` y `TestWalkthroughShowsCoverage/medida`, que aprobaban con `internal/c.go` sin abrir: que lo lean, o que pasen a REJECTED donde se verifica el registro. (R1, R2, R5, R8, R9)
- [ ] T5 Instrucciones del reviewer en `internal/agents/craft/reviewer.md` y `bflow render` (`render --check` en verde). Además, `docs/guia.md:34` y `CHANGELOG.md`. (R12, R13)
