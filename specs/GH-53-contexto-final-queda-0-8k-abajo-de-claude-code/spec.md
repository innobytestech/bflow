# GH-53 · Contexto final queda ~0.8k abajo de Claude Code: salida de la última llamada incompleta

## Brief

**Objetivo**: que el "final" de cada corrida de un subagente de Claude Code sea la cifra que Claude Code registra al terminar el agente (`totalTokens`), a menos de 100 tokens.

**Lo que se vio en el repo** (scout de GH-19, `agent-aea14e3dc438b2561`):
- La última fila de `calls.jsonl` da 8 + 1,083 + 50,697 = 51,788 (lo que bflow muestra: 51.8k). `Call.Context()` no suma la salida.
- Aunque la sumara, no alcanza: en el transcript del subagente, todas las líneas del último mensaje traen `output_tokens: 1`. Claude Code nunca escribe ahí la salida real del último mensaje.
- La cifra real solo aparece en el transcript de la sesión principal, en la línea `user` con `toolUseResult` del Task: `agentId`, `totalTokens: 52323` y `usage` de la última llamada (output 535). 8 + 1,083 + 50,697 + 535 = 52,323: Claude Code sí suma la salida al contexto.
- Fusionar sumando es correcto: el lector ya guarda deltas sobre el máximo acumulado de cada mensaje, así que la suma da el mayor. No se cambia.

**Entra**
- El lector de Claude Code saca de la sesión principal una muestra "final" por cada `toolUseResult` que tenga `agentId` y `usage`, y la asigna a la corrida `claude:agent-<agentId>`.
- Esa muestra se guarda en `calls.jsonl` con `final: true`. No es una fila ni suma tokens a la fase. `Runs` la usa como `FinalContext`.
- Sin muestra final (OpenCode, sesión principal, datos viejos), `FinalContext` = `Context()` de la última fila + su `Output`, como pide la decisión de GH-11.

**No entra**
- Corregir los totales de salida, que en cada corrida quedan cortos por la salida del último mensaje (~500). Va como seguimiento.
- `last_context` de las entradas `tokens` (GH-11), que tiene el mismo hueco.
- Recalcular datos viejos.

**Decisiones nuevas**
- [N] Fuente del final: `toolUseResult.usage` del transcript principal. Descartado sumar solo `Output` a la última fila: queda a ~530 de Claude Code porque el transcript del agente dice `output: 1`. Descartado también un hook PostToolUse: es un hook nuevo para el mismo dato.
- [N] El final es `Input + CacheRead + CacheWrite + Output` de `usage`, que es igual a `totalTokens`. Se usa `usage` y no `totalTokens` para que la cuenta no dependa de un campo agregado que Claude Code podría quitar.
- [N] Muestra final con el campo `Final bool` en `Sample` y en `Call`, más `Run` en `Sample` para apuntar a otra corrida. Descartado un archivo aparte: obliga a otro lector y a otro lock.

**Riesgos**
- La cifra del issue (52.5k) viene del mapa de agentes en vivo; la registrada es 52,323. El criterio se mide contra `totalTokens` ("Done (… tokens …)").
- El `toolUseResult` llega después del SubagentStop. Se lee en el siguiente Stop o SubagentStop; mientras tanto, el final usa la regla de respaldo.
- Si Claude Code cambia el formato de `toolUseResult`, no se emite la muestra y queda el respaldo. No hay error.

**Tamaño**: S-M (lector de claude, dos escritores de `calls.jsonl`, `Runs`).

## Discovery

No aplica (carril light).

## Requirements

Van dentro de cada tarea (carril light).

## Design

Ver Tasks.

## Tasks

- [ ] **T1 · Muestra final en el lector de Claude Code** (`internal/adapters/agent/claude/transcript.go`, `internal/metrics/metrics.go`)
  - `metrics.Sample` suma `Run string` (si no está vacío, es la corrida a la que va la muestra) y `Final bool`.
  - En `readFile`, una línea `type: "user"` con `toolUseResult.agentId` no vacío y `toolUseResult.usage` con algún valor > 0 emite `Sample{TS, Msg: "final:"+agentId, Run: metrics.RunKey("claude", "agent-"+agentId), Final: true, Usage{Input, Output, CacheRead, CacheWrite}}`, con `Calls` en 0. No toca `cur.Seen`. El filtro `bytes.Contains("usage")` ya deja pasar esas líneas.
  - C1: CUANDO el transcript principal trae un `toolUseResult` con `agentId` y `usage`, el lector DEBE emitir una sola muestra final con esos cuatro campos y la corrida del agente.
  - C2: CUANDO `toolUseResult` no existe, no es objeto, o le faltan `agentId` o `usage`, el lector NO DEBE emitir nada para esa línea ni fallar.
  - Pruebas en `transcript_test.go`: `TestReadUsageFinalFromToolUseResult`, `TestReadUsageToolUseResultWithoutUsage`.

- [ ] **T2 · Guardar la muestra final sin sumar tokens** (`internal/cli/metricscmds.go`, `internal/metrics/calls.go`)
  - `metrics.Call` suma `Final bool json:"final,omitempty"`.
  - En `addTokens`, las muestras con `Final` no van a `Allot`, `Phases`, las entradas `tokens` ni `e.AddTokens`. Solo escriben una línea `Call{TS, Run: s.Run, Tool, Agent: name, Msg, Input, CacheWrite, CacheRead, Output, Final: true}` en `calls.jsonl`. `addUnassigned` hace lo mismo en `.bflow/metrics/calls.jsonl`. Las demás muestras siguen igual.
  - C3: CUANDO llega una muestra final, los totales de fase y de la tarea NO DEBEN cambiar.
  - C4: CUANDO llega una muestra final, `calls.jsonl` DEBE tener una línea con `final: true` y `run` = la corrida del agente.
  - Pruebas en `internal/cli/calls_test.go`: `TestHookTokensFinalSampleNotCounted`.

- [ ] **T3 · `FinalContext` en `Runs`** (`internal/metrics/calls.go`)
  - En `Runs`, las `Call` con `Final` no se fusionan ni son filas. Se guardan por `Run`, y si hay varias para la misma corrida gana la de `TS` mayor. Después de armar cada corrida con filas: si tiene final, `FinalContext` = `Input + CacheRead + CacheWrite + Output` de esa final. Si no, `FinalContext` = `Context()` + `Output` de la última fila ya fusionada.
  - Una final cuya corrida no tiene filas se ignora: no crea corrida.
  - Se actualiza el comentario de `Call.Context()`: el contexto máximo sigue sin la salida y el final la incluye.
  - C5: CUANDO una corrida tiene muestra final, `FinalContext` DEBE ser la suma de sus cuatro campos (caso GH-19: 52,323).
  - C6: CUANDO una corrida no tiene muestra final, `FinalContext` DEBE ser el contexto de la última fila más su salida fusionada (fragmentos `output 1` + `928` cuentan 929).
  - C7: `Total`, `MaxContext`, `AccRead`, `AccNew` y `N` de las filas NO DEBEN cambiar por una muestra final.
  - Pruebas en `internal/metrics/calls_test.go`: `TestRunsFinalFromToolUseResult`, `TestRunsFinalFallbackIncludesOutput`, `TestRunsFinalWithoutRowsIgnored`. Se ajustan las que esperan `FinalContext == 301` (`calls_test.go`) y `final_context == 2001` (`internal/cli/calls_test.go`) a la nueva regla.
