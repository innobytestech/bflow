# GH-28 · guard: evaluar el comando dentro de bash -c / sh -c

## Brief

**Objetivo:** que el guard aplique sus reglas de Bash al comando que va dentro de `bash|sh|zsh [-flags] -c "<cmd>"`, para cerrar el atajo `bash -c "bflow approve X"`.

**Entra**
- `guard.Segments` desenvuelve los segmentos que invocan un shell con `-c`: en lugar del segmento envoltorio devuelve los segmentos de `<cmd>` (recursivo, hasta 3 niveles de anidación).
- Shell reconocido por su nombre base sin distinguir mayúsculas: `bash`, `sh`, `zsh`, con ruta (`/` o `\`), con `.exe`, entre comillas, con prefijos `VAR=x` / `env [flags] VAR=x` (igual que `BflowSubcommand`).
- Flags antes de `-c`: sueltos (`-l -c`), agrupados (`-lc`, `-ec`) y `-o`/`+o`/`-O`/`+O` con su valor (`-o pipefail -c`).
- Aplica a todas las reglas de Bash (human_only, git_destructive, force_push, protected_branch, coauthor, bflow_pr, bflow_branch, frozen_test) y a `TaskScoped`, porque todas pasan por `Segments`.
- Quitar la regex `bflowFreeze` de internal/guard/guard.go (sin uso tras #27).

**No entra**
- Otros intérpretes o envoltorios: `powershell -Command`, `cmd /c`, `python -c`, `eval`, `xargs`, `$(...)`, scripts en archivo. Guard no es un sandbox.
- Un parser de shell real (comillas escapadas, heredocs). `segSplit` sigue partiendo en `&&`, `;`, `|` aunque estén entre comillas, como hoy.

**Decisiones nuevas**
- [N] El desenvolvimiento vive en `Segments`, no solo en `bash()`. Se descarta hacerlo solo en `bash()`: `TaskScoped` usa `Segments` para decidir si carga la fase, y `bash -c "gh pr create"` saldría sin fase y pasaría `bflow_pr`. Efecto lateral aceptado: `review.FromAction` también ve los comandos de dentro (`bash -c "cat x"` cuenta como lectura de x).
- [N] Sin comilla inicial, `<cmd>` es todo el resto del segmento tras `-c` (más estricto que bash, que solo ejecuta la primera palabra). Se descarta tomar solo la primera palabra: negar de más es inocuo aquí.
- [N] Tope de 3 niveles de anidación; más allá el segmento se devuelve sin desenvolver. Se descarta recursión sin tope por costo y por entradas patológicas.

**Riesgos**
- Flags largos con valor antes de `-c` (`bash --rcfile x -c ...`) no se reconocen: el segmento queda sin desenvolver. Se acepta.
- Cambia la medición de lecturas del reviewer (GH-20) para comandos envueltos; es más exacta, no menos.

**Tamaño:** S. Unas 50 líneas en guard.go y unas 40 de pruebas.

## Discovery

Carril light, sin discovery aparte. `Segments` (internal/guard/guard.go) parte por `segSplit` y lo usan `bash()`, `TaskScoped` (llamado por `needsTask` en internal/cli/hookcmds.go) y `review.FromAction` (internal/review/review.go). `BflowSubcommand` ya trae la lógica de prefijos `VAR=x`/`env` y del nombre base del ejecutable. `bflowFreeze` solo se declara.

## Requirements

Carril light: los criterios van dentro de Tasks.

## Design

- Helper nuevo no exportado `unwrapShell(seg string) (inner string, ok bool)`:
  1. Tokens con `strings.Fields`, saltando prefijos `VAR=x` y `env [flags|VAR=x]` (extraer de `BflowSubcommand` a un helper compartido `skipPrefix(t []string) []string`, sin cambiar su comportamiento).
  2. El primer token, sin comillas, con `\` → `/`, base tras la última `/`, minúsculas, sin `.exe`, debe ser `bash`, `sh` o `zsh`.
  3. Recorre los tokens siguientes mientras empiecen con `-` o `+`: `-o`/`+o`/`-O`/`+O` consumen el siguiente token; un token `^-[A-Za-z]+$` que contenga `c` marca el fin de flags. Si se acaba sin `-c`, `ok=false`.
  4. `inner` = el texto del segmento original después del token `-c`, recortado. Si empieza con `"` o `'`, se quita esa comilla y, si termina con la misma, también la final (tolera la comilla sin cerrar que deja `segSplit`).
- `Segments(cmd)` pasa a `segments(cmd, depth)`: por cada segmento recortado, si `depth < 3` y `unwrapShell` da `ok`, agrega `segments(inner, depth+1)`; si no, agrega el segmento. La firma pública `Segments(cmd string) []string` no cambia.
- Seguridad: solo endurece; ninguna regla se relaja. La sesión principal sigue pudiendo correr `bash -c "bflow approve X"` porque `human_only` para approve solo aplica a subagentes.

## Tasks

- [ ] T1 Desenvolver `bash|sh|zsh -c` en `guard.Segments` (internal/guard/guard.go) y sus pruebas en internal/guard/guard_test.go.
  - C1 CUANDO un subagente corre `bash -c "bflow approve X"`, el guard DEBE negarlo con `human_only`; CUANDO lo corre la sesión principal, DEBE permitirlo.
  - C2 CUANDO el segmento es `sh -c 'git reset --hard'`, `bash -lc "git push -f origin x"` o `/usr/bin/zsh -c "git push origin main"` (main protegida), el guard DEBE negarlo con `git_destructive`, `force_push` y `protected_branch` respectivamente.
  - C3 El guard DEBE reconocer el shell con ruta `/` o `\`, `.exe`, mayúsculas, comillas en el token, prefijos `VAR=x`/`env`, flags agrupados (`-ec`) y `-o pipefail` antes de `-c`.
  - C4 CUANDO hay anidación (`bash -c "sh -c 'bflow approve X'"`) de hasta 3 niveles, el guard DEBE evaluar el comando más interno.
  - C5 `TaskScoped("bash -c \"gh pr create\"")` DEBE ser true.
  - C6 CUANDO el segmento no es un shell con `-c` (`bash script.sh`, `bashx -c x`, `echo bash -c x`, `bash -x script.sh`), `Segments` DEBE devolverlo sin cambios; los casos actuales de `TestBflowSubcommand`, `TestHumanOnlySubagent` y `TestBashRules` siguen pasando.
  - Pruebas: `TestShellWrapped` (C1, C2, C4, C5) y `TestSegmentsUnwrap` (C3, C6).
- [ ] T2 Quitar la regex `bflowFreeze` de internal/guard/guard.go. C7: `go vet ./...` y `go test ./internal/guard/...` pasan.
