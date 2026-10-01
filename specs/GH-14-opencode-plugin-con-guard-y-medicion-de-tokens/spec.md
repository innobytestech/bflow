# GH-14 · OpenCode: plugin con guard y medición de tokens

## Brief

**Objetivo:** que una sesión de OpenCode en un repo con `agent:` que incluye opencode tenga el mismo guard y el mismo conteo de tokens por fase, agente y modelo que Claude Code, mediante un plugin generado por `bflow render` que solo le pasa los datos a bflow.

**Entra**
- `bflow render` genera `.opencode/plugins/bflow.js` (con la marca de generado, se commitea y nunca pisa un archivo ajeno).
- Guard: `tool.execute.before` de bash/edit/write/multiedit/patch/apply_patch llama a `bflow guard --tool opencode`; con exit 2 bloquea con el motivo.
- Tokens: el plugin agrega cada mensaje de asistente terminado a `.bflow/cache/opencode/<sessionID>.jsonl`; en `session.idle` se corre una vez `bflow hook tokens --tool opencode`, que atribuye por fase, agente y modelo y, en la sesión principal, avisa como el Stop de Claude.
- doctor: estado del plugin en lugar del warn "llegan con GH-14" y nota de lo que OpenCode no cubre.
- Columna OpenCode en la tabla de garantías del README; adapters/opencode/README.md y docs/guia.md.
- Captura real de eventos con OpenCode 1.1.53 como fixture de pruebas (D4).

**No entra:** nudge del subagente que termina sin reportar en OpenCode (no hay evento); hook de inicio de sesión; versión mínima de OpenCode.

**Decisiones del discovery:** D1 Go traduce la entrada nativa (no JS); D2 fail-open; D3 JSONL por sesión y un proceso por turno; D4 captura real antes de construir; D5 el plugin resuelve la sesión padre y el agente.

**Decisiones nuevas**
- [N1] Bandera `--tool opencode` en `guard` y `hook tokens`; sin bandera, el camino de Claude queda idéntico. Descartado: autodetectar la herramienta por la forma del JSON (adivina y mezcla los dos formatos).
- [N2] Interfaz nueva `cli.HookAdapter` (ParseActions, TokenSources, ReadUsage) que implementa solo OpenCode. Descartado: que OpenCode implemente todo `AgentAdapter` con métodos vacíos, y cambiar la firma de `ParsePreToolUse` de Claude.
- [N3] Un patch de varios archivos produce una acción por ruta; basta una bloqueada. Descartado: evaluar solo la primera.
- [N4] El modelo se guarda como `providerID/modelID`, igual que los valores de `models.opencode`. Descartado: solo `modelID` (dos proveedores con el mismo modelo se mezclan).
- [N5] Los ids vistos de OpenCode se guardan en el cursor con prefijo `opencode:`. Descartado: sin prefijo (comparte el mapa con los de Claude).
- [N6] En el idle de la sesión principal se borran los JSONL leídos por completo y sin cambios en 7 días, con su offset. Descartado: no limpiar (la carpeta crece sin límite).
- [N7] La nota de doctor sobre lo que OpenCode no cubre es `ok` informativa, no `warn`. Descartado: `warn` (es una limitación permanente que no se puede corregir y enseña a ignorar doctor).
- [N8] El nombre del sessionID se sanea a `[A-Za-z0-9_-]` en el plugin y en Go antes de usarlo como nombre de archivo.

**Riesgos**
- OpenCode cambia eventos o nombres de herramientas sin versión mínima: la captura (D4) fija lo que se probó, doctor muestra la versión y el fallo es abierto (la herramienta pasa).
- Con bflow fuera del PATH el guard no protege: el plugin avisa una vez por sesión.
- Si OpenCode no carga plugins desde `.opencode/plugins/` en 1.1.53, la captura lo detecta en T1 antes de construir.

**Tamaño:** L. 7 tareas; adaptador OpenCode (guard y tokens), cli (guard, hook tokens, render, doctor), un plugin JS embebido y documentación.

## Discovery

GH-13 (ya en main) dejó el adaptador de OpenCode con render, comando /bflow y alias; guard y tokens solo existen para Claude (`c.Agent`). Decisiones con el humano: Go traduce la entrada nativa (D1), fail-open (D2), JSONL por sesión con un proceso por turno (D3), captura real de eventos antes de construir (D4) y resolución de subagente en el plugin (D5). Discovery completo en el tracker.

## Requirements

Plugin y render
- R1 [D] CUANDO `agent:` incluye opencode, `bflow render` DEBE escribir `.opencode/plugins/bflow.js` con el contenido embebido de `adapters/opencode/plugin.js`, que lleva `// ` + `agents.GeneratedMark` en su primera línea; `render --check` DEBE cubrirlo; si existe sin la marca DEBE dar `render_conflict`; si opencode sale de `agent:`, el generado DEBE salir como `stale` y borrarse.
- R2 [D] El plugin NO DEBE importar paquetes npm: solo lo que da OpenCode (`client`, `directory`), Bun y módulos `node:` incluidos en Bun.

Guard
- R3 [D] CUANDO OpenCode va a ejecutar bash, edit, write, multiedit, patch o apply_patch, el plugin DEBE correr `bflow guard --tool opencode` (cwd = `directory`) con un JSON por stdin `{tool, args, sessionID, agent, subagent, cwd}`; para cualquier otra herramienta NO DEBE lanzar proceso.
- R4 [D] CUANDO `bflow guard` sale con código 2, el plugin DEBE lanzar un `Error` con el stderr recortado, lo que bloquea la herramienta.
- R5 [D] CUANDO bflow no está en el PATH, tarda más de 10 s (se mata el proceso) o sale con cualquier código distinto de 0 y 2, el plugin DEBE dejar pasar la herramienta y avisar una sola vez por sesión que el guard no está activo.
- R6 [D] `ParseActions` DEBE traducir: bash → `guard.Bash` con `args.command`; edit y multiedit → `guard.Edit` con `args.filePath`; write → `guard.Write` con `args.filePath`; patch/apply_patch → una acción por archivo del texto del patch (`*** Add File:` → Write; `*** Update File:`, `*** Delete File:`, `*** Move to:` → Edit). Una ruta relativa DEBE resolverse contra `cwd`. Una herramienta desconocida DEBE dar cero acciones (se permite). Un JSON sin `tool` DEBE dar `ok=false`.
- R7 [N] CUANDO hay varias acciones, `bflow guard` DEBE evaluarlas en orden y bloquear con la primera que el guard rechaza (registro y motivo de esa).
- R8 [D] Cada acción DEBE llevar `Subagent` y `Agent` de la entrada. El plugin DEBE resolver la sesión: caché sessionID → {parentID, agent} desde eventos; si falta, una consulta a `client.session.get`; si aun así no se sabe, sesión principal (`subagent:false`, `agent:""`).
- R9 [D] CUANDO la config de bflow está rota o no hay repo bflow, `bflow guard --tool opencode` DEBE permitir (exit 0, sin salida), como hoy.
- R10 [N] Sin `--tool`, `guard` y `hook tokens` DEBEN comportarse exactamente como hoy (Claude). Con un `--tool` que no está registrado o que no implementa `HookAdapter`, `guard` DEBE permitir y `hook tokens` salir callado, ambos con exit 0.

Tokens
- R11 [D] CUANDO llega `message.updated` de un mensaje de asistente con `time.completed`, el plugin DEBE agregar una línea completa a `.bflow/cache/opencode/<sessionID saneado>.jsonl` con solo metadatos: `{id, sessionID, parentID, agent, providerID, modelID, created, completed, tokens}`; nunca el contenido del mensaje.
- R12 [D] CUANDO llega `session.idle`, el plugin DEBE correr una vez `bflow hook tokens --tool opencode` con stdin `{sessionID, parentID, cwd}`, sin esperar el resultado para seguir y sin avisar errores.
- R13 [D] En el idle de una sesión principal (`parentID` vacío), `hook tokens` DEBE procesar todos los JSONL de la carpeta (incluidos hijos sin idle propio) y después avisar como el Stop de Claude (`notifyActive`); en el idle de un hijo DEBE procesar solo su archivo y no avisar.
- R14 [D] Los tokens de un archivo DEBEN atribuirse a `main` si su sesión no tiene `parentID`; si lo tiene, al `agent` sin el prefijo `bflow-`; con `parentID` y sin agente, a `?`.
- R15 [D] El uso DEBE mapearse: input → Input, output + reasoning → Output, cache.read → CacheRead, cache.write → CacheWrite; `TS` = `completed` (ms); `model` = `providerID/modelID` [N4]; `tool` en la entrada `tokens` = `opencode`.
- R16 [D] Un mismo `id` en varias líneas DEBE contarse como una llamada con el uso más alto visto; ids guardados en el cursor como `opencode:<id>` [N5].
- R17 [D] El lector DEBE consumir solo líneas completas y avanzar el offset por archivo en el cursor existente (`tokens-cursor.json`, con su lock).
- R18 [N] En el idle de la sesión principal, los JSONL cuyo offset llegó al tamaño y sin cambios en 7 días DEBEN borrarse junto con su offset.
- R19 [D] Sin repo bflow, con la config rota o sin tarea activa, `hook tokens --tool opencode` DEBE salir callado con exit 0.

Doctor y documentación
- R20 [D] CUANDO `agent:` incluye opencode, doctor DEBE reportar el plugin: `ok` al día; `warn` si falta o difiere ("corre bflow render"); `fail` si existe sin la marca. El warn "llegan con GH-14" DEBE desaparecer.
- R21 [D] doctor DEBE agregar una línea `ok` [N7] que diga que OpenCode no tiene nudge de subagente ni hook de inicio y que lo cubren `bflow report`, `bflow check --verify` y el bloque de AGENTS.md.
- R22 [D] El README DEBE tener columna OpenCode en la tabla de garantías (guard y tokens sí; nudge de subagente no, con la razón); adapters/opencode/README.md y docs/guia.md DEBEN describir el plugin y no mencionar GH-14 como pendiente.
- R23 [D] Las pruebas del adaptador DEBEN usar la captura real de OpenCode 1.1.53 guardada en `internal/adapters/agent/opencode/testdata/`; si el runtime difiere de `types.gen.d.ts`, manda el runtime.

## Design

### Interfaz nueva (internal/cli/cli.go)

```go
// TokenSource es un archivo de uso y de quién es ("" = sesión principal).
type TokenSource struct{ Path, Agent string }

// HookAdapter es lo que guard y hook tokens necesitan de una herramienta que
// no es la de c.Agent (OpenCode). Se elige con --tool.
type HookAdapter interface {
	Name() string
	// ParseActions traduce la entrada nativa en acciones; ok=false si no tiene la forma.
	ParseActions(raw []byte) (acts []guard.Action, cwd string, ok bool)
	// TokenSources dice qué archivos leer en este evento y si es la sesión principal.
	TokenSources(raw []byte, cacheDir string) (srcs []TokenSource, main bool)
	ReadUsage(path string, cur *metrics.Cursor) ([]metrics.Sample, error)
	// Prune borra los archivos leídos por completo y viejos, y sus offsets.
	Prune(cacheDir string, cur *metrics.Cursor, olderThan time.Duration)
}

// hooks devuelve la herramienta de --tool si implementa HookAdapter.
func (c *Ctx) hooks(name string) HookAdapter
```

`AgentAdapter` y el adaptador de Claude no cambian. `opencode.Agent` sigue siendo `ToolAdapter` y agrega `HookAdapter`.

### cli
- `guard` y `hook tokens` declaran `--tool` (texto, vacío por defecto). Vacío → código actual. Con valor → `c.hooks(name)`; nil → permitir / callado (R10).
- `runGuard`: con HookAdapter, `ParseActions` → por cada acción, `needsTask` y `guard.Evaluate`; la primera rechazada se registra con `logGuard` y sale `Rejected` (R7). Se extrae `guardContext(cfg)` para compartir el armado de `guard.Context`.
- `runHookTokens`: se extrae `addTokens(e, tool, samples, agent)` (Allot, entradas `tokens`, AddTokens) que usan los dos caminos. Con HookAdapter: `cacheDir = <store>/cache/opencode`; bajo el lock del cursor, por cada `TokenSource` → ReadUsage → addTokens; si `main`, `Prune(cacheDir, &cur, 7*24h)` y `defer notifyActive(e)`.
- `doctorOpenCode`: usa `planRender`; `.opencode/plugins/bflow.js` en `Conflicts` → fail; en `Changed` → warn (falta o difiere, según exista); si no → ok. Más la nota R21. Se quita la línea GH-14.

### Adaptador OpenCode (internal/adapters/agent/opencode)
- `hook.go`: `type guardInput struct{ Tool string; Args json.RawMessage; SessionID, Agent, Cwd string; Subagent bool }` (claves JSON `tool, args, sessionID, agent, subagent, cwd`). `func (Agent) ParseActions(raw []byte) ([]guard.Action, string, bool)`; `func patchPaths(text string) (writes, edits []string)`.
- `transcript.go`: `type usageLine struct{ ID, SessionID, ParentID, Agent, ProviderID, ModelID string; Created, Completed int64; Tokens struct{ Input, Output, Reasoning int64; Cache struct{ Read, Write int64 } } }`. `TokenSources`: valida `sessionID` con `^[A-Za-z0-9_-]+$` (si no, sin fuentes); main → todos los `*.jsonl` de cacheDir; hijo → solo el suyo. El agente de cada archivo sale de su primera línea completa (R14). `ReadUsage` sigue el mismo algoritmo que `claude.readFile` (offset, líneas completas, delta contra `Seen`, truncado → 0). `Prune` (R18).
- `RenderAgents` agrega `".opencode/plugins/bflow.js": files.Plugin`. `GeneratedAgents` incluye el plugin si existe y contiene la marca.
- Comentario del paquete: quitar "llegan con GH-14".

### Plugin (adapters/opencode/plugin.js, embebido como `files.Plugin`)
- `export const BflowPlugin = async ({ client, directory }) => ({ ... })`, ≤100 líneas.
- `event`: `session.created/updated` → caché {parentID}; `message.updated` (rol assistant con `time.completed`) → caché del agente (`info.agent ?? info.mode`) y append con `node:fs.appendFileSync` de una línea terminada en `\n`; `session.idle` → `Bun.spawn` de `hook tokens` sin esperar.
- `tool.execute.before`: filtro por nombre; `Bun.which("bflow")` (null → aviso, pasa); `Bun.spawn` con stdin del JSON, temporizador de 10 s que mata el proceso; exit 2 → `throw new Error(stderr.trim())`; otro → aviso único por sesión (`client.tui.showToast`, y si falla `console.error`) y pasa.
- Los datos van por stdin, nunca interpolados en un comando (sin inyección de shell).
- Lo que se confirme con la captura (nombre del campo de agente, forma de `args` del patch, carpeta `plugins/` frente a `plugin/`) manda sobre esta sección; el implementer lo anota en Design si cambia.

### Superficie de seguridad
- Authz: el plugin no decide; toda regla está en `guard.Evaluate`, la misma de Claude. Un subagente mal detectado se trata como sesión principal (más restrictiva con StrictLeader).
- Fail-open deliberado (D2): queda visible por el aviso de sesión y por doctor.
- Validación: sessionID saneado antes de usarlo como nombre de archivo (no se sale de la carpeta); Go solo lee `*.jsonl` de `cacheDir`; JSON malformado → línea ignorada.
- Datos sensibles: el JSONL no guarda texto de mensajes ni argumentos; vive en `.bflow/`, ignorada por git; el log del guard trunca el comando como hoy.
- Errores: ningún hook de OpenCode bloquea por un fallo de bflow, solo por un rechazo explícito (exit 2).

## Tasks

- [ ] T1 Contrato (R23 y firmas de todo). Correr `opencode run` (1.1.53) en un repo temporal con un plugin que vuelca eventos y guardar en `internal/adapters/agent/opencode/testdata/` `events.jsonl` (message.updated, session.idle, session.created de hijo) y `tool_before.jsonl` (bash, edit, write, patch/apply_patch). Declarar `cli.TokenSource`, `cli.HookAdapter`, `Ctx.hooks` y en el adaptador `ParseActions`, `TokenSources`, `ReadUsage`, `Prune` con cuerpo mínimo, y escribir las pruebas que fallan:
  - opencode: `TestParseActionsBash`, `TestParseActionsEditWrite`, `TestParseActionsPatchVariasRutas`, `TestParseActionsRutaRelativa`, `TestParseActionsHerramientaIgnorada`, `TestParseActionsEntradaInvalida`, `TestParseActionsCaptura`, `TestTokenSourcesPrincipalTodos`, `TestTokenSourcesHijoSoloSuyo`, `TestTokenSourcesSessionIDInvalido`, `TestReadUsageMapeo`, `TestReadUsageMismoIDUnaVez`, `TestReadUsageLineaIncompleta`, `TestReadUsageCaptura`, `TestPruneViejosLeidos`, `TestRenderIncluyePlugin`, `TestPluginSinDependencias`.
  - cli: `TestGuardOpenCodeBloquea`, `TestGuardOpenCodePatchUnaBloqueada`, `TestGuardOpenCodeSubagente`, `TestGuardOpenCodeConfigRota`, `TestGuardToolDesconocida`, `TestGuardSinToolIgualQueAntes`, `TestHookTokensOpenCodePrincipal`, `TestHookTokensOpenCodeHijo`, `TestHookTokensOpenCodeSinTarea`, `TestRenderPluginConflicto`, `TestRenderPluginStale`, `TestDoctorOpenCodePlugin`.
- [ ] T2 Guard de OpenCode: `ParseActions` y `patchPaths`; `--tool` en `guard`, `guardContext`, evaluación por acción. (R6, R7, R8, R9, R10)
- [ ] T3 Tokens de OpenCode: `TokenSources`, `ReadUsage`, `Prune`; `--tool` en `hook tokens`, `addTokens` compartido, notify solo en principal. (R13, R14, R15, R16, R17, R18, R19, R10)
- [ ] T4 Plugin: `adapters/opencode/plugin.js` embebido, render y `GeneratedAgents` lo incluyen. (R1, R2, R3, R4, R5, R8, R11, R12)
- [ ] T5 doctor: estado del plugin y nota de cobertura; quitar la línea GH-14 y ajustar su prueba en `internal/cli/opencode_test.go`. (R20, R21)
- [ ] T6 Documentación: columna OpenCode en el README, adapters/opencode/README.md (tabla con el plugin, sin "Pendiente (GH-14)"), docs/guia.md y el comentario del paquete. (R22)
- [ ] T7 Prueba de punta a punta con OpenCode real en un repo temporal con bflow: `git reset --hard` bloqueado con el motivo, una edición permitida, tokens visibles en `bflow metrics` atribuidos a main y a un agente `bflow-*`; con `bflow` fuera del PATH la herramienta pasa y aparece el aviso. Anotar el resultado en el traspaso. (R3, R4, R5, R13, R14)
