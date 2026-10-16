# GH-17 · guard sugiere bflow check --quick en lugar de go test directo

## Brief

**Objetivo**: cuando un agente corre la prueba del stack directo (`go test ./...`, `npm test`...), el guard le sugiere `bflow check --quick <paquete>` sin bloquear el comando.

**Entra**
- Función pura `guard.Advise(a Action) string` que reconoce runners de prueba en cada segmento (incluido dentro de `bash -c`, vía `Segments`).
- El aviso sale por el hook posterior al Bash (`bflow guard --post`), el canal que GH-86 ya usa para avisar sin bloquear: en Claude, stderr con exit 2 en PostToolUse (el modelo lo lee); en OpenCode, el plugin ya lo agrega a `output.output`.
- Una vez por sesión de agente (marca en `.bflow/cache/advice/`), solo si `check.quick` está configurado.

**No entra**
- Bloquear el comando ni reescribirlo.
- Pasos `quick` por defecto para npm u otros stacks (hoy solo Go los trae, `internal/setup/detect.go:57`).
- Cambios a los adaptadores (`claude/hook.go`, plugin de OpenCode): el canal posterior ya existe.

**Decisiones nuevas**
- [N1] Aviso en el hook posterior, no en el previo. Descartado: campo `Advice` en `guard.Decision` + `additionalContext` en PreToolUse; exige render nuevo en ambos adaptadores y OpenCode no tiene forma de dar contexto sin bloquear antes de la herramienta.
- [N2] Solo se avisa si `cfg.Check.Quick` no está vacío. Descartado: avisar siempre; en un repo sin pasos rápidos `bflow check --quick` no corre nada y el consejo sería falso.
- [N3] Una vez por sesión (`Action.Session`; sin sesión, cada vez). Descartado: cada vez; repite el mismo texto en cada `go test -run X` de depuración y gasta tokens.
- [N4] Lista fija de runners: `go test`, `npm test`, `npm run test`, `pnpm test`, `yarn test`, `npx vitest|jest`, `pytest`, `python -m pytest`, `cargo test`, `dotnet test`. Descartado: derivarla de `check.quick`; los pasos quick son plantillas con `{pkg}` y no siempre coinciden con lo que teclea el agente.
- [N5] Si el mismo Bash restauró pruebas congeladas (GH-86), gana ese aviso y la sugerencia se omite.

**Riesgos**
- Claude rotula el exit 2 de PostToolUse como error del hook; el texto debe dejar claro que el comando sí corrió ("aviso, no bloqueo").
- Falsos positivos (`echo "go test"`, `grep go test`): la detección mira el ejecutable al inicio del segmento, no subcadenas.

**Tamaño**: S. Un paquete (`guard`) + `runGuardPost` en `internal/cli/hookcmds.go`; ~120 líneas con pruebas.

## Discovery

<!-- Discovery completo; lo escribe bflow al cerrar el gate discovery. No lo edites. -->

## Requirements

<!-- Criterios EARS numerados R1..Rn, cada uno [D] (del discovery) o [N] (nuevo). -->

## Design

<!-- Estructuras, firmas, decisiones tomadas y descartadas, superficie de seguridad. -->

## Tasks

- [ ] T1 · Contrato. `func Advise(a Action) string` en `internal/guard/advise.go` (nuevo) y su prueba de tabla `TestAdvise` en `internal/guard/advise_test.go`; `TestGuardPostAdvice`, `TestGuardPostAdviceOncePerSession`, `TestGuardPostAdviceNoQuick` y `TestGuardPostFrozenWins` en `internal/cli/guard_post_test.go` (pueden fallar hasta T3).
- [ ] T2 · Detección. `Advise` devuelve "" salvo que `a.Tool == Bash` y algún segmento de `Segments(a.Command)` empiece (tras asignaciones `VAR=x` y ejecutable con ruta o entre comillas, como en GH-39) con un runner de [N4]. Criterios:
  - CUANDO el comando es `go test ./...`, `cd x && npm test` o `bash -c "pytest -q"`, `Advise` DEBE devolver un texto no vacío que nombre el comando detectado y `bflow check --quick <paquete>`.
  - CUANDO el runner solo aparece como argumento (`echo go test`, `grep -r "npm test" .`, `git commit -m "go test"`), `Advise` DEBE devolver "".
  - CUANDO el segmento es un comando `bflow` (`BflowSubcommand != ""`), `Advise` DEBE devolver "".
  - El texto DEBE decir que el comando sí corrió y caber en una línea (≤200 caracteres), sin emojis.
- [ ] T3 · Canal. En `runGuardPost` (`internal/cli/hookcmds.go`), después de la revisión de pruebas congeladas y solo si no hubo restauración ([N5]): si `len(cfg.Check.Quick) > 0` y `guard.Advise(*bash) != ""`, escribir `bflow guard: <aviso>` en stderr y devolver un envelope `OK: true`, `Code: "advice"`, `Data: {"reason": aviso}`, `Quiet: !c.JSON`, con `WithExit(output.ExitRejected)`. No llamar a `logGuard` (no es un rechazo). Hoy `runGuardPost` sale temprano si no hay tarea activa o la fase no congela: reordenar para que el aviso no dependa de la tarea. Criterios:
  - CUANDO un Bash con `go test ./...` termina y `check.quick` está configurado, `bflow guard --post` DEBE salir con código 2 y el aviso en stderr (Claude y `--tool opencode`).
  - CUANDO `check.quick` está vacío, DEBE salir con 0 y sin salida.
  - CUANDO el mismo Bash restauró pruebas congeladas, DEBE salir solo con el aviso de GH-86.
  - El guard sigue abierto: config rota, entrada no reconocida o error al escribir la marca DEBEN dar `allowed`.
- [ ] T4 · Una vez por sesión ([N3]). Marca vacía `.bflow/cache/advice/<sesión saneada>` (mismo saneo que los nombres de `loops`); si existe, no se avisa. Limpiar marcas viejas con el mismo corte que `.bflow/cache/loops`. Criterios:
  - CUANDO la misma sesión corre `go test` dos veces, solo la primera DEBE recibir el aviso.
  - CUANDO `Action.Session` está vacío, DEBE avisarse cada vez.
- [ ] T5 · Docs. Una línea en `docs/conceptos.md` (donde se describe el guard) y entrada en el changelog: el guard sugiere `bflow check --quick` tras correr la prueba del stack directo.
