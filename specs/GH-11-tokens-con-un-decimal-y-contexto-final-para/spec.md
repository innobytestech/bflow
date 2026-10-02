# GH-11 · Tokens con un decimal y contexto final, para cuadrar con Claude Code

## Brief

**Objetivo**: que las cifras de tokens de `bflow watch`, el panel y `bflow metrics` se puedan comparar con el agent map de Claude Code: miles redondeados a un decimal y el contexto final de cada agente a la vista.

**Entra**
- `metrics.Human` redondea los miles a un decimal (15,916 → `15.9k`) en lugar de truncar (`15k`). Afecta a nuevos, caché, contexto máximo y a todo lo que ya usa `Human` (setup/doctor incluido).
- Nuevo campo `Usage.LastContext`: contexto final = la última llamada del agente, `input + cache_read + cache_write + output`. Es la cifra que el agent map llama "tokens" del subagente (`subagent_tokens` del harness).
- Se calcula en los lectores de transcript (Claude y OpenCode), se guarda en la entrada `tokens` del log como `last_context` y se agrega en `metrics.Compute`.
- Se muestra en `metrics.Detail` (watch y panel) y como columna `ctx final` en la tabla por agente/modelo de `bflow metrics`.

**No entra**
- Cambiar qué cuenta como "nuevos" o "caché" (ya cuadran al token, medido en GH-6).
- Recalcular el contexto final de tareas ya registradas: los datos viejos no tienen `last_context` y simplemente no lo muestran.
- `bflow calls` (ya muestra cifras exactas).

**Decisiones nuevas**
- [N] Siempre un decimal en miles (`184.0k`), igual que ya pasa con millones (`1.0M`). Descartado: quitar el `.0`, que hace que la columna cambie de ancho y que `184k` parezca truncado.
- [N] Si el redondeo llega a `1000.0k`, se muestra `1.0M`.
- [N] Contexto final por "el último gana": `Allot` pone `LastContext` solo en la parte (fase, modelo) que contiene la muestra cronológicamente última; `Usage.Add` sobrescribe cuando `o.LastContext > 0`. Como el log se lee en orden, gana la última descarga. Descartado: guardar el timestamp de la última llamada en `Usage` (más campos y más JSON para el mismo resultado).
- [N] El contexto final incluye la salida (así lo mide el agent map); el contexto máximo sigue sin ella, como hoy.

**Riesgos**
- Las líneas de transcript de un mismo mensaje llegan en partes: el contexto final debe usar los valores acumulados del mensaje (`max(u, prev)`), no el delta.
- Pruebas y textos que esperan `184k` o `14k nuevos` cambian.

**Tamaño**: S (un paquete, dos lectores, tres pantallas).

## Discovery

Carril light, sin discovery. Formato en `internal/metrics/metrics.go` (`Human`, `Detail`, `Usage.Add`, `usageOf`, `Allot`); muestras en `internal/adapters/agent/{claude,opencode}/transcript.go`; registro en `internal/cli/metricscmds.go` (`addTokens`, `usageTable`); pantallas en `internal/cli/watch.go` y `internal/cli/ui.go` vía `Detail`.

## Requirements

<!-- Carril light: los criterios van en Tasks. -->

## Design

<!-- Carril light: ver Brief y Tasks. -->

## Tasks

- [ ] **T1 Redondeo a un decimal.** `metrics.Human(n)`: `n < 1000` → `"900"`; `1000 ≤ n` → `fmt.Sprintf("%.1fk", float64(n)/1e3)`; si el resultado redondeado es ≥ 1000.0k o `n ≥ 1_000_000` → `"%.1fM"`.
  - CUANDO n = 15,916, `Human` DEBE devolver `15.9k`; con 184,300 → `184.3k`; con 999,960 → `1.0M`; con 1,250,000 → `1.2M`; con 900 → `900`.
  - Actualizar `TestHuman`/las aserciones de `metrics_test.go` y cualquier otra prueba que fije el formato viejo.
- [ ] **T2 Contexto final en las muestras.** Agregar `LastContext int64 \`json:"last_context,omitempty"\`` a `metrics.Usage` con su comentario. En `claude/transcript.go` y `opencode/transcript.go`, cada muestra lleva `LastContext` = input + cache_read + cache_write + output **acumulados del mensaje** (los mismos que se guardan en `cur.Seen`).
  - CUANDO un mensaje llega en varias líneas, la última muestra de ese mensaje DEBE llevar el contexto con la salida completa.
  - Prueba en cada lector con un mensaje partido en dos líneas.
- [ ] **T3 Agregación "el último gana".** `Usage.Add`: si `o.LastContext > 0`, `u.LastContext = o.LastContext`. En `Allot`, solo la `Share` que contiene la muestra de índice mayor (la última del transcript) conserva `LastContext`; las demás lo dejan en 0. `addTokens` escribe `last_context` en la entrada `tokens` cuando es > 0, y `usageOf` lo lee.
  - CUANDO un agente tiene llamadas en dos fases, `TaskStats.Agents[agente].LastContext` DEBE ser el de su última llamada, no el máximo ni la suma.
  - CUANDO las entradas `tokens` no traen `last_context` (datos viejos), DEBE quedar 0 y nada falla.
  - Pruebas en `metrics_test.go` para `Allot` + `Compute`.
- [ ] **T4 Mostrarlo.** `metrics.Detail`: con llamadas, `"<nuevos> nuevos · <n> llamadas de hasta <máx> · final <final>"`; el tramo `· final …` se omite si `LastContext == 0`. `usageTable` en `metricscmds.go` agrega la columna `ctx final` (con `tok`, que da `-` si es 0). watch y panel lo heredan de `Detail`.
  - CUANDO un agente tiene `LastContext` = 15,916, watch y panel DEBEN mostrar `final 15.9k` en su línea.
  - Actualizar la prueba de `Detail` (`14.0k nuevos · 2 llamadas de hasta 140.0k`, más el caso con final).
- [ ] **T5 Docs.** Entrada en `CHANGELOG.md` (redondeo y contexto final) y, si el README describe la línea por agente o la tabla de `bflow metrics`, actualizar el ejemplo. Verificar que `go test ./...` pasa.
