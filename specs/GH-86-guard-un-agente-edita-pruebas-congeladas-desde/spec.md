# GH-86 · Guard: un agente edita pruebas congeladas desde Bash (python/sed/heredoc) sin que lo bloquee

## Brief

**Objetivo:** en fases congeladas, una prueba congelada que un agente reescribe desde Bash se niega antes de correr el comando o, si se escapa, se restaura y se avisa en ese mismo paso.

**Entra**
- Guard (PreToolUse) de Bash: niega con `frozen_test` `sed -i`/`perl -pi`, redirección `>`/`>>`, `tee`, destino de `cp`/`install`, `git apply`/`patch` cuyo parche nombra la ruta, e intérpretes (python, perl, ruby, node…) con `-c`/`-e` o heredoc que mencionan la ruta congelada. Las lecturas pasan.
- Copia de cada prueba al congelar (`.bflow/tasks/<ID>/frozen-tests/<hash>`); `bflow freeze` y el re-congelado al DONE la actualizan.
- `bflow guard --post`: después de cada Bash, con tarea activa en fase congelada, compara hashes, restaura la prueba cambiada (o borra la que no existía) y le devuelve al agente el motivo `frozen_test`.
- Hooks: `PostToolUse` (matcher `Bash`) en `adapters/claude/settings.json`; `tool.execute.after` en el plugin de OpenCode 1.x y `execute.after` en el de 2.x.
- README de los dos adaptadores y CHANGELOG.

**No entra**
- Parser de shell completo; escrituras desde herramientas que no son Bash ni Edit/Write.
- Cambiar la detección al DONE (`frozenRejection`) ni `check --verify`: siguen como red final.
- Volver a etiquetar `git checkout -- …`/`git restore`: ya los niega `git_destructive`, que es más estricta.

**Decisiones nuevas**
- [N] Copia direccionada por hash (`frozen-tests/<sha256>`, nombre = hash ya guardado en `frozen-tests.json`). Descartado: copia por ruta espejo (choca al re-congelar y hay que mapear rutas a archivos).
- [N] Los cuerpos de heredoc de intérpretes se revisan sobre el comando crudo: `Segments` los quita (solo conserva los que alimentan un shell). Descartado: dejar de quitarlos en `Segments` (metería texto de scripts en todas las reglas de Bash y abriría falsos positivos en `git_destructive`, `bflow_pr`, etc.).
- [N] Un intérprete con heredoc/`-c` que solo *lee* la prueba también se niega si menciona la ruta. Descartado: analizar el script. El motivo dice que lea con `cat`/`grep`.
- [N] `git apply`/`patch`: el guard lee el archivo del parche (hasta 1 MiB, relativo a la raíz) y busca la ruta; si no puede leerlo, deja pasar y lo atrapa `--post`.
- [N] El límite de líneas de `bflow.js` (OpenCode 1.x) sube de 100 a 120 en sus pruebas: hoy tiene 100 exactas y el hook nuevo no cabe.

**Riesgos**
- Una persona que edita una prueba congelada a mano mientras un agente corre Bash pierde su cambio: `--post` la restaura. Lo correcto sigue siendo `bflow freeze --allow` o `bflow freeze`.
- `execute.after` de OpenCode 2.x no está probado (GH-98 solo validó `execute.before`); T1 lo captura. Si no existe, el implementer reporta NEEDS_DECISION.
- Tareas congeladas antes de este cambio no tienen copia: `--post` solo avisa.
- Costo: `--post` corre tras cada Bash; sin tarea activa o fuera de fase sale tras leer config y estado.

**Tamaño:** L (unas 700 líneas con pruebas: guard.go, engine/freeze.go, cli/hookcmds.go, settings.json, dos plugins, READMEs, CHANGELOG).

## Discovery

Mecanismo doble: el guard niega en PreToolUse una lista acotada de patrones de escritura y un chequeo posterior a cada Bash restaura y avisa lo que se escape. La restauración usa una copia guardada al congelar, validada contra el hash; una prueba `absent` se borra. Con `freeze --allow` vigente no se niega ni se restaura. Funciona igual en Claude y OpenCode. Lecturas (cat, grep, go test, sed sin -i, git diff) nunca se niegan. El discovery completo está en el tracker.

## Requirements

- R1 [D] CUANDO la tarea activa está en una fase congelada (`implementing`, `paused`, `quality`, `documenting`, `walkthrough`) y un segmento de Bash corre `sed` o `perl` con un flag corto agrupado que contiene `i` (`-i`, `-i.bak`, `-pi`, `-Ei`) o `--in-place`, y uno de sus argumentos es una prueba congelada, el guard DEBE negarlo con la regla `frozen_test`.
- R2 [D] CUANDO en esa fase un segmento redirige la salida (`>`, `>>`, `1>`, `2>`, `&>`, `>|`, con o sin espacio antes del destino) hacia una prueba congelada, el guard DEBE negarlo con `frozen_test`.
- R3 [D] CUANDO en esa fase un segmento corre `tee` (con o sin `-a`) con una prueba congelada como argumento, el guard DEBE negarlo con `frozen_test`.
- R4 [D] CUANDO en esa fase un segmento corre `cp` o `install` y su último argumento sin guion es una prueba congelada, el guard DEBE negarlo con `frozen_test`. `mv` sigue cubierto por la regla de hoy.
- R5 [D] CUANDO en esa fase un segmento corre `git apply` o `patch` y el archivo del parche (argumento o `<`) o el cuerpo de heredoc que lo alimenta menciona una prueba congelada, el guard DEBE negarlo con `frozen_test`.
- R6 [D] CUANDO en esa fase un intérprete (python, python3, py, perl, ruby, node, deno, bun, php, pwsh, powershell) recibe código por `-c`/`-e` o por heredoc (directo o por `cat <<EOF | python`), y ese texto menciona una prueba congelada, el guard DEBE negarlo con `frozen_test`.
- R7 [N] CUANDO un argumento de R1, R3 o R4 trae comodines (`*`, `?`, `[`), el guard DEBE tratarlo como una prueba congelada si el patrón coincide con alguna.
- R8 [D] CUANDO el comando solo lee la prueba (cat, grep, rg, head, tail, less, `sed` sin `-i`, `go test`, git diff/show/log, `cp <congelada> <otro>`, redirección `< <congelada>`), el guard NO DEBE negarlo.
- R9 [D] CUANDO la fase no es congelada, o la prueba tiene la marca `allow`, el guard NO DEBE aplicar R1 a R6.
- R10 [D] El motivo de R1 a R6 DEBE nombrar la ruta y decir lo mismo que el de Edit/Write (NEEDS_DECISION, `bflow freeze --allow <ruta>`); en R6 suma que para leerla use `cat` o `grep`.
- R11 [N] CUANDO el comando Bash podría escribir (contiene `>`, `sed`, `perl`, `tee`, `cp`, `install`, `apply`, `patch` o un intérprete de R6), `bflow guard` DEBE cargar fase y pruebas congeladas de la tarea activa para evaluar R1 a R6.
- R12 [D] CUANDO bflow congela pruebas (aprobar el contrato), re-congela (`bflow freeze`) o re-congela las `allow` al DONE, DEBE guardar los bytes de cada prueba existente en `frozen-tests/<hash>` junto a `frozen-tests.json` y borrar las copias que ya no referencia.
- R13 [D] CUANDO `bflow guard --post` recibe un Bash y hay tarea activa en fase congelada, DEBE comparar el hash de cada prueba congelada (sin `allow` ni hash vacío) con el guardado y, si cambió, restaurarla con la copia; si el hash guardado es `absent`, DEBE borrarla.
- R14 [D] Antes de restaurar, `--post` DEBE validar que el hash normalizado de la copia sea el guardado; si falta la copia o no coincide, NO DEBE escribir y DEBE avisar que no pudo restaurarla.
- R15 [D] CUANDO `--post` restaura, borra o no puede restaurar, DEBE salir con código 2, escribir en stderr el motivo `frozen_test` (archivos, qué hizo, NEEDS_DECISION / `freeze --allow`), registrar el rechazo como el guard (`logGuard`) y anotar en el log de la tarea el evento `frozen_restore` con los archivos.
- R16 [D] CUANDO no hay tarea activa, la fase no es congelada, la acción no es Bash, la config no carga o algo falla, `--post` DEBE salir con código 0 sin salida y sin tocar archivos.
- R17 [D] La restauración DEBE escribir los bytes guardados tal cual (CRLF incluido); la comparación usa `testHash` (normaliza CRLF).
- R18 [D] Claude: `adapters/claude/settings.json` DEBE tener `PostToolUse` con matcher `Bash` y comando `bflow guard --post` (timeout 10).
- R19 [D] OpenCode 1.x: `bflow.js` DEBE tener `tool.execute.after` que, para `bash`, mande el mismo cuerpo que `before` a `bflow guard --tool opencode --post`, espere hasta 10 s y, con código 2, agregue el stderr al final de `output.output`. Con otro código distinto de 0 avisa una vez y no interrumpe.
- R20 [D] OpenCode 2.x: `bflow2/index.js` DEBE hacer lo mismo con `ctx.tool.hook("execute.after")` para `shell`, entregando el motivo al modelo por el campo de salida que T1 capture.
- R21 [N] La detección al reportar DONE (`FrozenChanged`/`frozenRejection`) y `check --verify` NO DEBEN cambiar.

## Design

### guard (internal/guard/guard.go)

- `func FrozenPhase(p flow.Phase) bool`: exporta la pertenencia a `frozenPhases`; `isFrozenPhase` la usa.
- `func MayWrite(cmd string) bool` (R11): `mayWriteRe = (?i)(>|\b(sed|perl|tee|cp|install|apply|patch|python[0-9.]*|py|ruby|node|deno|bun|php|pwsh|powershell)\b)`.
- `var interpreters = map[string]bool{...}` (R6). `exeName` + quitar sufijo de versión (`python3.12` → `python`).
- `func frozenWrite(s string, c Context) (path string, read bool)`: evalúa un segmento ya separado. Tokeniza con `strings.Fields` y quita comillas de cada token. Orden:
  1. Redirecciones (R2): token que empieza por `>`, `>>`, `1>`, `2>`, `&>`, `>|` con resto no vacío, o igual a uno de ellos y destino en el siguiente token. Destinos `&1`/`&2` se ignoran. `<` no cuenta.
  2. `sed`/`perl` (R1): primer token tras `skipPrefix`; in-place si algún token `-[A-Za-z]*i…` (para `perl` y `sed`, flag corto cuya parte de letras contiene `i` minúscula) o `--in-place*`. Revisa todos los tokens sin guion.
  3. `tee` (R3): todos los tokens sin guion.
  4. `cp`/`install` (R4): último token sin guion.
  5. `git apply`/`patch` (R5): tokens sin guion tras el subcomando y el de `<`; lee cada uno con `readPatch`.
  6. Intérprete con `-c`/`-e` (R6): el segmento entero se busca con `mentions`; `read=true` para elegir el motivo.
- `func matchFrozen(arg string, c Context) string`: `rel(c.Root, arg)`; si contiene `*?[`, `path.Match(arg, f)` contra cada congelada; si no, igualdad. Devuelve la congelada o "".
- `func mentions(text string, frozen []string) string`: reemplaza `\` por `/` y devuelve la primera congelada contenida como subcadena.
- `func readPatch(c Context, p string) []byte`: `os.ReadFile` (relativa a `c.Root` si no es absoluta), máximo 1 MiB (`io.LimitReader`); error → nil.
- `func heredocWrites(cmd string, c Context) (path string, read bool)` (R5, R6): recorre el comando crudo línea a línea con `heredocRe` como `stripHeredocs`; si algún segmento de la línea de cabecera (`segSplit`) tiene como ejecutable un intérprete, `git apply` o `patch`, junta el cuerpo y aplica `mentions`. Se llama sobre `a.Command` y también sobre cada cuerpo desenvuelto por `unwrapShell` (vía `Segments` del texto de `bash -c`, que ya conserva heredocs que alimentan un shell).
- En `bash()`, después del bloque `rmRe` y solo si `isFrozenPhase(c)`: primero `heredocWrites(a.Command, c)`, luego `frozenWrite(s, c)` por segmento. El primero que encuentra ruta devuelve `deny("frozen_test", motivoFrozenBash(p, read))`.
- Motivos (R10): constante compartida con `edit()`: `"%s quedó congelada al aprobar el contrato. Si la prueba está mal, reporta NEEDS_DECISION con el cambio que necesitas; si una persona lo aprueba y corre `bflow freeze --allow %s`, podrás cambiarla una vez."`; R6 suma `" Para leerla usa cat o grep."`.
- `c.Frozen` ya excluye las `allow` (`Engine.Frozen`), lo que cumple R9 sin código nuevo.

### engine (internal/engine/freeze.go)

- `const FrozenSnapshots = "frozen-tests"` (carpeta en la tarea).
- `saveFrozen` (ya lo llaman `writeFrozen`, `Refreeze` vía `writeFrozen`, `refreezeAllowed` y `AllowFrozen`) pasa a: escribir `frozen-tests.json`; por cada valor que no es `absent`, `allow` ni "", si no existe `frozen-tests/<hash>`, copiar los bytes actuales de la prueba solo si `testHash(f) == hash`; borrar archivos de `frozen-tests/` cuyo nombre no está entre los valores (R12). `AllowFrozen` no crea copias nuevas (marca `allow`) y conserva las de las demás.
- `type FrozenRestore struct { Restored, Deleted, Missing []string }` con `func (r FrozenRestore) Empty() bool`.
- `func (e *Engine) RestoreFrozen(id string) FrozenRestore` (R13, R14, R17): por cada `f, h` de `readFrozen` con `h` distinto de "" y `allow`, si `testHash(f) != h`: con `h == absent` → `os.Remove` → `Deleted`; si no, lee `frozen-tests/<h>`, valida el hash normalizado y escribe con `store.WriteAtomic` (creando la carpeta) → `Restored`; si falta o no valida → `Missing`. Listas ordenadas. Si no está vacío, `Store.Append` evento `frozen_restore` con `{"restored","deleted","missing"}`.
- `func frozenRestoreReason(r FrozenRestore) string`: "`<archivos>` son pruebas congeladas y el último comando las cambió: bflow las restauró (o borró, si no existían al congelar). No las cambies; si una prueba está mal, reporta NEEDS_DECISION. Si una persona aprueba el cambio, que corra bflow freeze --allow <archivo> desde su terminal." Para `Missing`: "no hay copia para restaurar `<archivos>`: déjalos como estaban (git diff) antes de reportar DONE."

### cli (internal/cli/hookcmds.go)

- Flag nuevo en `guard`: `fs.Bool("post", false, "hook posterior a Bash: restaura pruebas congeladas que cambiaron y avisa al agente")`.
- `runGuard`: si `post`, llama `runGuardPost(c, acts, cwd)` tras parsear (Claude: `ParsePreToolUse` sirve, PostToolUse trae `tool_name`, `tool_input` y `cwd`; OpenCode: `ParseActions`). Nunca devuelve `Fail` (R16): entrada no reconocida → `allowed`.
- `runGuardPost`: sin acción `guard.Bash` → `allowed`. `config.Load`, `c.Build`, `e.Active`, `e.Status`; `!guard.FrozenPhase(v.Phase)` → `allowed`. `r := e.RestoreFrozen(id)`; vacío → `allowed`. Si no: stderr `"bflow guard: "+reason`, `logGuard(c, root, "frozen_test", a)`, `output.Rejected("frozen_test", reason)` con `Quiet = !c.JSON` (exit 2). No llama `recordReads` ni `markSession`.
- `needsTask` (R11): en Bash suma `|| guard.MayWrite(a.Command)`.

### Adaptadores

- `adapters/claude/settings.json`: `"PostToolUse": [{"matcher": "Bash", "hooks": [{"type": "command", "command": "bflow guard --post", "timeout": 10}]}]`. `mergeSettings` ya fusiona eventos nuevos; `.claude/settings.json` del repo se actualiza con `bflow install claude`.
- `adapters/opencode/plugin.js`: `"tool.execute.after": async (input, output)` solo con `input.tool === "bash"`; cuerpo `{tool, args, sessionID, agent, subagent, cwd}`; como `after` no trae los argumentos, `before` los guarda en un `Map` por `input.callID` y `after` los toma y los borra (sin entrada en el `Map`, manda `args: {}`: `--post` no los necesita para decidir); `run(["guard","--tool","opencode","--post"], body)`, temporizador 10 s; código 2 → `output.output = (output.output ?? "") + "\n\n" + err.trim()`; nunca lanza.
- `adapters/opencode/plugin2/index.js`: `ctx.tool.hook("execute.after", async (x) => ...)` para `shell`, mismo cuerpo con `x.input`, y el motivo en el campo de salida capturado en T1.
- READMEs: una línea en cada adaptador sobre el hook posterior.

### Seguridad

- Authz: `--post` solo restaura a la versión que bflow guardó al congelar; no acepta rutas de la entrada (lee las de `frozen-tests.json`), así que un agente no puede dirigir la escritura. Los nombres de copia son hashes hex de 64 caracteres; `RestoreFrozen` descarta valores que no lo sean antes de armar la ruta (evita `../`).
- Validación: la copia se valida contra el hash antes de escribir; si `frozen-tests.json` o la copia fueron alterados de forma inconsistente, no se escribe nada.
- Errores: `--post` es abierto como el guard: cualquier fallo sale 0 sin bloquear. El guard PreToolUse sigue igual de abierto.
- Datos sensibles: las copias son pruebas del repo, quedan en `.bflow/` (fuera de git).

### Descartado

- Solo PostToolUse (más barato): avisaría pero el agente ya gastó el paso; el discovery pidió ambos.
- Negar cualquier comando que mencione la ruta congelada: rompe `go test`, `cat`, `git diff`.

## Tasks

- [x] T1 Contrato (R1-R21). Firmas vacías que compilan: `guard.FrozenPhase`, `guard.MayWrite`, `engine.FrozenSnapshots`, `engine.FrozenRestore`, `(*Engine).RestoreFrozen`, flag `--post` en `guard` que por ahora sale `allowed`. Capturar en OpenCode 2.0.22 el `x` de `execute.after` para `shell` en `internal/adapters/agent/opencode/testdata/tool_after_v2.jsonl` y el `(input, output)` de `tool.execute.after` 1.x en `tool_after.jsonl`; si 2.x no tiene `execute.after`, reportar NEEDS_DECISION. Subir el límite de `bflow.js` de 100 a 120 en `plugin_test.go`. Escribir las pruebas con estos nombres:
  - `internal/guard/guard_test.go`: `TestBashFrozenWrites` (tabla: sed -i, sed -i.bak, sed --in-place, perl -pi, `>`, `>>`, `2>`, `&>`, tee, tee -a, cp destino, install destino, `sed -i … internal/*_test.go`, `python - <<'EOF'` con la ruta, `cat <<EOF | python`, `python -c`, `node -e`, `git apply x.diff` con la ruta en el archivo, `patch -p1 < x.diff`, `git apply <<EOF`, `bash -c "sed -i … "`, ruta absoluta bajo `Root`), `TestBashFrozenReads` (cat, grep, rg, head, tail, `sed -n`, `go test ./...`, git diff, `cp <congelada> /tmp/x`, `wc < <congelada>`, `2>&1`, `python x.py`), `TestBashFrozenOutsidePhase` (contract y sin congeladas), `TestBashFrozenReason`, `TestMayWrite`, `TestFrozenPhase`.
  - `internal/engine/freeze_test.go`: `TestFrozenSnapshotSaved`, `TestFrozenSnapshotRefreeze` (Refreeze y refreezeAllowed actualizan y podan), `TestRestoreFrozen` (restaura, CRLF intacto, evento `frozen_restore`), `TestRestoreFrozenAbsent`, `TestRestoreFrozenAllowSkipped`, `TestRestoreFrozenNoSnapshot` (tarea vieja → `Missing`), `TestRestoreFrozenBadSnapshot`, `TestRestoreFrozenBadHashName`.
  - `internal/cli/guard_post_test.go`: `TestGuardPostRestores` (Claude, exit 2, stderr con `frozen_test`), `TestGuardPostOpenCode`, `TestGuardPostNoTask`, `TestGuardPostOutsidePhase`, `TestGuardPostNotBash`, `TestGuardPostBadInput`, `TestNeedsTaskMayWrite`.
  - `adapters/claude/adapter_test.go`: `TestSettingsPostToolUse`. `internal/adapters/agent/opencode/plugin_test.go`: `TestPluginExecuteAfter`, `TestPlugin2ExecuteAfter`.
  Las que todavía no aplican llevan `t.Skip("T<n>")`. Al terminar, build y pruebas pasan.
- [x] T2 Guard PreToolUse (R1-R11): `FrozenPhase`, `MayWrite`, `frozenWrite`, `matchFrozen`, `mentions`, `readPatch`, `heredocWrites`, motivo compartido con `edit()`, `needsTask` con `MayWrite`. Quita los skip de las pruebas de guard y `TestNeedsTaskMayWrite`.
- [x] T3 Copias al congelar (R12): `saveFrozen` guarda y poda `frozen-tests/`. Quita los skip de `TestFrozenSnapshot*`.
- [x] T4 Restauración (R13-R17, R21): `RestoreFrozen`, `frozenRestoreReason`, `runGuardPost`. `FrozenChanged` y `frozenRejection` sin cambios. Quita los skip de `TestRestoreFrozen*` y `TestGuardPost*`.
- [x] T5 Hooks (R18-R20): `settings.json`, `plugin.js` (`tool.execute.after` con `Map` por `callID`), `plugin2/index.js` (`execute.after`); correr `bflow render` y `bflow install claude` en este repo. Quita los skip de `TestSettingsPostToolUse` y `TestPlugin*ExecuteAfter`.
- [x] T6 Docs: README de `adapters/claude` y `adapters/opencode`, entrada en CHANGELOG (Agregado: el guard niega escrituras a pruebas congeladas desde Bash y las restaura si se escapan; hace falta `bflow install claude` y `bflow render`).
