# GH-84 · Tokens de la sesión principal a mitad de turno: el panel no los muestra hasta el Stop

## Brief

**Objetivo**: que los tokens de la sesión principal aparezcan en el panel cada vez que termina un subagente, sin esperar al Stop del turno.

**Entra**
- Claude Code: el hook SubagentStop (que ya corre `bflow hook tokens`) lee, además del transcript del subagente, el de la sesión principal (`transcript_path`, que ya viene en la entrada del hook).
- OpenCode: el `hook tokens --tool opencode` de una sesión hija lee también el archivo de uso de su sesión padre (`<cacheDir>/<parentID>.jsonl`), que el plugin ya escribe paso a paso.
- Mismo cursor (`cache/tokens-cursor.json`), misma atribución por marca y hora (R5-R7 de GH-51) y misma regla de no avanzar el cursor si falla una escritura (R10 de GH-51).
- Docs: `docs/comandos.md` (fila `hook tokens`), `docs/marcas-sesion.md` y quitar #84 de los pendientes conocidos en `docs/como-probar-y-reportar.md`.

**No entra**
- Turnos largos sin subagentes (solo comandos): siguen viéndose al Stop.
- El aviso a la persona (`notifyActive`) y la poda de marcas: siguen solo en el Stop / idle de la sesión principal.
- Cambios al plugin de OpenCode ni a `adapters/claude/settings.json`: los hooks actuales bastan.

**Decisiones nuevas**
- [N1] Disparar en SubagentStop. Descartado: PostToolUse de Bash/bflow, porque correría en cada comando, tomaría el lock del cursor (hasta 5 s) en la ruta crítica de cada herramienta y se sumaría a la latencia de `guard --post`.
- [N2] En Claude, `AgentAdapter.TokenSource(raw) TokenSource` pasa a `TokenSources(raw) []TokenSource` (subagente primero, principal después). Descartado: una segunda llamada a `tokensFrom` por fuente, porque tomaría el lock dos veces y podría guardar el cursor de una fuente y no de la otra.
- [N3] "Sesión principal" (avisa y poda marcas) = ninguna fuente tiene `Agent`; en SubagentStop eso es falso aunque se lea el transcript principal.

**Riesgos**
- Lecturas repetidas del mismo transcript principal por subagentes en paralelo: las cubre el cursor por ruta (offset + `Seen` por id de mensaje); una respuesta a medio escribir se completa en la siguiente pasada con el delta, sin contar otra llamada.
- Latencia de SubagentStop: un transcript principal más bajo el mismo lock; se lee solo desde el offset.

**Tamaño**: S (dos adaptadores, `runHookTokens`, pruebas y docs).

## Discovery

Carril light: sin discovery. Ver scout (`bflow show GH-84 scout`).

## Requirements

En el carril light los criterios van dentro de Tasks.

## Design

En el carril light las decisiones van en el Brief.

## Tasks

- [x] T1 Claude: `TokenSources` y lectura del principal en SubagentStop.
  - `internal/cli/cli.go`: en `AgentAdapter`, `TokenSource(raw []byte) TokenSource` se reemplaza por `TokenSources(raw []byte) []TokenSource`.
  - `internal/adapters/agent/claude/transcript.go`: en Stop devuelve `[{Path: transcript_path, Session}]`; en SubagentStop devuelve `[sub, {Path: transcript_path, Session: session_id}]`; omite fuentes con `Path` vacío.
  - `internal/cli/metricscmds.go` `runHookTokens`: sin fuentes, sale callado; `main` = ninguna fuente tiene `Agent`; `notifyActive` solo si `main`; una sola llamada a `tokensFrom` con todas las fuentes.
  - R1: CUANDO termina un subagente en Claude Code, el sistema DEBE sumar las llamadas nuevas de la sesión principal hasta ese momento, repartidas por marca y hora como en GH-51 R5.
  - R2: CUANDO llega el Stop después de uno o más SubagentStop, el sistema DEBE sumar solo las llamadas principales no contadas antes: ninguna llamada se cuenta dos veces.
  - R3: CUANDO falla una escritura en SubagentStop, el sistema NO DEBE avanzar el cursor de ninguna de las dos fuentes (GH-51 R10).
  - R4: CUANDO corre SubagentStop, el sistema NO DEBE avisar a la persona ni podar marcas.
  - R5: CUANDO la entrada de SubagentStop no trae `transcript_path`, el sistema DEBE sumar solo al subagente, como hoy.
  - Pruebas: `TestTokenSources` (renombra `TestTokenSource` en `internal/adapters/agent/claude/transcript_test.go`; casos Stop, SubagentStop con y sin `agent_transcript_path`, sin `transcript_path`); en `cmd/bflow/e2e_marks_test.go`: `TestHookTokensMainMidTurn` (R1, R2: SubagentStop y luego Stop dan el total exacto), `TestHookTokensMidTurnCursorHeld` (R3), `TestHookTokensMidTurnNoNotify` (R4).
- [x] T2 OpenCode: la sesión hija lee también a su padre.
  - `internal/adapters/agent/opencode/transcript.go` `TokenSources`: con `parentID` válido (`sessionRe`), agrega `<cacheDir>/<parentID>.jsonl` después del archivo propio; `main` sigue en false.
  - R6: CUANDO termina la ejecución de una sesión hija en OpenCode, el sistema DEBE sumar también las llamadas nuevas de su sesión padre, sin duplicarlas en el idle del padre.
  - R7: CUANDO `parentID` no cumple `sessionRe` o su archivo no existe, el sistema DEBE sumar solo a la hija.
  - Pruebas: `TestOpenCodeTokensChildReadsParent` y `TestOpenCodeTokensBadParentID` en `internal/cli/opencode_hooks_test.go`.
- [x] T3 Docs (R1, R6): `docs/comandos.md` (SubagentStop también suma la sesión principal), `docs/marcas-sesion.md` (cuándo se carga la principal) y quitar la línea de #84 en `docs/como-probar-y-reportar.md`.
