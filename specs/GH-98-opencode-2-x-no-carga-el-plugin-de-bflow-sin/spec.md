# GH-98 · OpenCode 2.x no carga el plugin de bflow: sin guard, sin tokens y sin estado al abrir sesión

## Brief

**Objetivo**: que OpenCode 2.x cargue un plugin de bflow con guard, tokens e inyección del estado, sin romper el `bflow.js` que usa 1.x.

**Entra**
- Una fuente nueva embebida `adapters/opencode/plugin2/` (`index.js` + `package.json`) que `bflow render` escribe en `.opencode/plugins/bflow2/`. 2.x carga directorios; 1.x los ignora.
- El plugin 2.x hace lo mismo que el de 1.x: guard antes de cada herramienta vigilada, aviso si bflow no está en el PATH, caché de tokens `.bflow/cache/opencode/<sid>.jsonl` con la misma línea, `bflow hook tokens` al terminar la ejecución e inyección del estado en la sesión raíz.
- En Go, `bflow guard --tool opencode` acepta los nombres y argumentos de 2.x (`shell`, `subagent`, `patch`, `path`).
- Render, `GeneratedAgents`, la detección de conflicto, `doctor` y el borrado de obsoletos cubren los dos archivos nuevos.
- Fixtures reales capturados de 2.0.22, pruebas y docs (README del adaptador, docs/, CHANGELOG).

**No entra**: más diagnóstico de doctor para 2.x ni docs de problemas comunes (#99), el líder cuando falla un subagente (#100), el mapeo de modelos (#96). `bflow.js` no cambia.

**Decisiones del discovery**: dos destinos (descartados: un archivo dual, porque 1.x se cae, y elegir por versión, porque Desktop 2.x y CLI 1.x conviven); la traducción de nombres va en Go; límites de 100 y 160 líneas.

**Decisiones nuevas**
- [N] El `id` de cada línea de caché es el `id` del evento `session.step.ended`, no `assistantMessageID`. Descartado `assistantMessageID`: si dos pasos comparten mensaje, `ReadUsage` (que se queda con el máximo por id) contaría uno solo.
- [N] `package.json` lleva la marca de generado en `description`. Así render y doctor lo tratan igual que a los demás archivos generados. Descartado: tratarlo como archivo sin marca, lo que pediría una excepción en `planRender`.
- [N] Cuando render borra `bflow2/` por obsoleto, también borra la carpeta si queda vacía. Si queda, 2.x intenta cargarla y deja un WARN.
- [N] `bflow hook tokens` no cambia: la línea de 2.x es igual a la de 1.x. Solo se agrega una prueba con una línea 2.x.

**Riesgos**
- La API de 2.x no está documentada y puede cambiar entre versiones 2.0.x. Mitigación: fixtures reales en testdata y validación manual en 2.0.22.
- Faltan por confirmar con captura (T1): los argumentos de `subagent`, `patch` y `read` (offset/limit), y las unidades de `ev.created` y `data.started` (se esperan ms).
- El `session.created` de la raíz puede llegar antes de suscribirse. Por eso el `parentID` también se pide con `ctx.session.get` y la inyección sale del hook `prompt`, no del evento.

**Tamaño**: M. Unas 160 líneas de JS, unas 60 de Go y pruebas; 7 tareas.

## Discovery

OpenCode 2.x exige `export default {id, setup}`, y 1.x llama como función a cada export, así que un mismo archivo no sirve para las dos. 2.x carga directorios con `package.json` dentro de `.opencode/plugins/` y 1.x los ignora. La API de 2.x se probó en 2.0.22: `ctx.tool.hook("execute.before")` bloquea si lanza un error; `ctx.event.subscribe()` devuelve un async iterable con `session.step.started/ended` y `session.execution.succeeded`; `ctx.session.hook("prompt")` más `ctx.session.synthetic` inyectan texto; el directorio del proyecto está en `ctx.location.directory`. Los nombres de las herramientas cambian: shell=bash, subagent=task, y los argumentos usan `path` en vez de `filePath`.

## Requirements

**Render, doctor y convivencia con 1.x**
- R1 [D] CUANDO `opencode` está en `agent:`, `bflow render` DEBE escribir `.opencode/plugins/bflow2/index.js` y `.opencode/plugins/bflow2/package.json` desde las fuentes embebidas, además de `.opencode/plugins/bflow.js`, sin cambios.
- R2 [D] CUANDO un archivo existe en `bflow2/index.js` o `bflow2/package.json` sin la marca de generado, `bflow render` DEBE fallar con `render_conflict` sin tocarlo, y `bflow doctor` DEBE marcar `plugin` en `fail`.
- R3 [D] CUANDO alguno de los tres archivos del plugin falta o difiere, `bflow doctor` DEBE marcar `plugin` en `warn` nombrando el archivo y pidiendo `bflow render`. CUANDO los tres están al día, DEBE marcar `ok`.
- R4 [N] CUANDO `opencode` sale de `agent:`, `bflow render` DEBE borrar los dos archivos de `bflow2/` como obsoletos y, si la carpeta queda vacía, borrarla también.
- R5 [D] El `index.js` de 2.x DEBE exportar `export default { id: "bflow", setup }`, importar solo módulos `node:` y tener 160 líneas o menos. `bflow.js` DEBE conservar `export const BflowPlugin` y tener 100 líneas o menos.

**Guard (plugin 2.x)**
- R6 [D] CUANDO 2.x dispara `execute.before` con `shell`, `edit`, `write`, `patch`, `read` o `subagent`, el plugin DEBE mandar por stdin `{tool, args, sessionID, agent, subagent, cwd}` a `bflow guard --tool opencode --reads`, esperar hasta 10 s y, con código 2, lanzar `Error(stderr)` para bloquear la herramienta. Con otro código distinto de 0 DEBE avisar una vez y dejar pasar la herramienta.
- R7 [D] CUANDO la herramienta es `subagent`, o es `read` fuera de una subsesión `bflow-reviewer`, el plugin DEBE lanzar el guard sin esperarlo (`stderr: "ignore"`) y dejar pasar la herramienta.
- R8 [D] CUANDO `bflow` no está en el PATH, el plugin DEBE avisar una sola vez por `console.error` y dejar pasar la herramienta sin lanzar procesos.
- R9 [D] El campo `agent` DEBE ser `x.agent` en una subsesión (la que tiene `parentID`) y `""` en la raíz. `subagent` DEBE ser `true` solo en subsesiones.

**Tokens (plugin 2.x)**
- R10 [D] CUANDO llega `session.step.ended`, `.bflow/` existe y el `sessionID` saneado no queda vacío, el plugin DEBE agregar a `.bflow/cache/opencode/<sid>.jsonl` la línea `{id, sessionID, parentID, agent, providerID, modelID, created, completed, tokens}`. Valores: `id` = `ev.id`; `providerID`, `modelID` y `agent` salen del `session.step.started` del mismo `assistantMessageID`; `created` = `data.started` de ese evento, o `ev.created` si no hubo; `completed` = `ev.created`; `tokens` = `data.tokens`.
- R11 [D] CUANDO llega `session.execution.succeeded`, el plugin DEBE lanzar `bflow hook tokens --tool opencode` con `{sessionID, parentID, cwd}` sin esperarlo.
- R12 [N] CUANDO `bflow hook tokens --tool opencode` lee una caché escrita por el plugin 2.x, DEBE contar cada paso como una llamada con el modelo `providerID/modelID`, igual que con 1.x.

**Inyección del estado (plugin 2.x)**
- R13 [D] CUANDO se dispara el hook `prompt` en una sesión raíz que aún no recibió la inyección, con `bflow` en el PATH y `.bflow/` presente, el plugin DEBE correr `bflow hook session-start` (stdin `{}`, 10 s como máximo) y, con código 0 y texto no vacío, llamar a `ctx.session.synthetic({sessionID, text})`. DEBE inyectar una sola vez por sesión y nunca en una subsesión.

**Robustez**
- R14 [D] CUANDO cualquier paso del plugin 2.x falla (spawn, lectura de sesión, escritura de caché o iteración de eventos), el plugin DEBE tragarse el error sin interrumpir a OpenCode. La única excepción es el bloqueo intencional del guard (R6).
- R15 [N] CUANDO OpenCode llama a la función de limpieza que devuelve `setup`, el plugin DEBE dejar de iterar los eventos.

**Go: guard con nombres de 2.x**
- R16 [D] `ParseActions` DEBE traducir `shell` a `guard.Bash`, `subagent` a `guard.Spawn` y `patch` con argumentos de 2.x a escrituras y ediciones, y DEBE leer la ruta de `path` cuando no hay `filePath` (en edit, write y read). Los nombres y argumentos de 1.x DEBEN dar las mismas acciones que hoy.

## Design

**Archivos y nombres**
- `adapters/opencode/plugin2/index.js`: la fuente nueva. La primera línea es `// ` + `agents.GeneratedMark`.
- `adapters/opencode/plugin2/package.json`: `{"name":"bflow","type":"module","main":"index.js","description":"<GeneratedMark>"}` (una línea y salto final).
- `adapters/opencode/embed.go`: se agregan `Plugin2 []byte` (`//go:embed plugin2/index.js`) y `Plugin2Package []byte` (`//go:embed plugin2/package.json`).
- `internal/adapters/agent/opencode/opencode.go`: se agregan las constantes `plugin2Dir = ".opencode/plugins/bflow2"`, `plugin2File = plugin2Dir + "/index.js"` y `plugin2Package = plugin2Dir + "/package.json"`, y la lista exportada `PluginFiles = []string{pluginFile, plugin2File, plugin2Package}`. `RenderAgents` escribe las tres rutas. `GeneratedAgents` recorre `PluginFiles` y agrega cada archivo que lleve la marca.
- `internal/cli/setupcmds.go` (doctor): reemplaza `plugin := ".opencode/plugins/bflow.js"` por un recorrido de `opencode.PluginFiles` con un solo check `plugin`. Gana el peor estado: `fail` si alguno está en `Conflicts`, `warn` si alguno está en `Changed` (el detalle nombra los archivos), `ok` si ninguno.
- `internal/cli/rendercmds.go`: después de `os.Remove` de un obsoleto, `os.Remove(filepath.Dir(...))` si el padre es `.opencode/plugins/bflow2`. Si la carpeta no está vacía, el error se ignora.
- `internal/adapters/agent/opencode/hook.go`: `args` gana `Path string \`json:"path"\`` y el campo de patch y de subagente que confirme el fixture de T1. Se agregan `case "shell"` (como bash) y `case "subagent"` (como task). `file := args.FilePath; if file == "" { file = args.Path }`.

**Forma de `index.js`** (≤160 líneas, solo `node:fs` y `node:path`)
```
const GUARDED = new Set(["shell","edit","write","patch","read","subagent"])
export default { id: "bflow", setup: async (ctx) => { ...; return () => { stop = true; it.return?.() } } }
```
- `dir = ctx.location.directory`. `sessions: Map<sid, {parentID, known}>`. `steps: Map<assistantMessageID, {providerID, modelID, agent, started}>`. `injected: Set<sid>`.
- `resolve(sid)` usa `ctx.session.get({sessionID})` y toma `r?.parentID ?? r?.data?.parentID ?? ""`.
- `run(args, input, wait = true)` es igual que en 1.x: `Bun.spawn(["bflow", ...args], {cwd: dir, ...})` con el JSON por stdin.
- `ctx.tool.hook("execute.before", async (x) => ...)` implementa R6 a R9.
- `ctx.session.hook("prompt", async (x) => ...)` implementa R13 y se envuelve en try/catch.
- `const it = ctx.event.subscribe(); (async () => { for await (const ev of it) { if (stop) break; try {...} catch {} } })().catch(() => {})`. No se espera dentro de `setup`. Atiende `session.created` (anota el parentID), `session.step.started`, `session.step.ended` y `session.execution.succeeded`.

**Decisiones**
- Tomada: un destino por versión (del discovery). Descartadas: el archivo dual y la elección por versión.
- Tomada: la traducción va en Go y el plugin manda `tool` y `args` sin tocarlos. Descartado traducir en JS, porque duplicaría las reglas fuera de `bflow guard`.
- Tomada: `id` = `ev.id`. Descartado `assistantMessageID` (ver Brief).
- Tomada: inyectar en el primer `prompt`. Descartado `session.created`, que puede llegar antes de suscribirse y no tiene un `noReply` probado en 2.x.
- Tomada: el aviso sale por `console.error`. Descartado buscar una TUI en `ctx`: no está confirmada.

**Superficie de seguridad**
- Authz: toda regla vive en `bflow guard`. El plugin solo transporta datos. El bloqueo es fail-open ante fallos del guard (como en 1.x) y fail-closed solo con código 2.
- Validación: el `sessionID` se sanea con `safe()` (`[A-Za-z0-9_-]`) antes de armar la ruta de caché, y `TokenSources` lo vuelve a validar con `sessionRe`. Los argumentos van por stdin como JSON, nunca interpolados en un shell, porque `Bun.spawn` recibe un arreglo.
- Errores: try/catch en cada handler, timeout de 10 s con `kill`, y `.catch` en el iterador de eventos.
- Datos sensibles: la caché guarda ids, modelo y conteos de tokens, sin contenido de prompts. El guard recibe los argumentos de la herramienta, igual que en 1.x. El texto inyectado es salida de `bflow hook session-start`.

**Pruebas (nombres)**
- `internal/adapters/agent/opencode/plugin_test.go`:
  - `TestRenderIncluyePlugin2`: render trae las 3 rutas con la marca; `GeneratedAgents` solo cuenta los archivos marcados.
  - `TestPlugin2SinDependencias`: literales `export default`, `id`, `setup`, `execute.before`, `session.step.ended`, `session.execution.succeeded`; solo `node:`; ≤160 líneas.
  - `TestPlugin2Guard`: GUARDED trae los 6 nombres; dos `"--reads"`, uno con `false` antes de `return`; `bflow-reviewer` en el filtro; `throw new Error` tras el código 2; `10000`.
  - `TestPlugin2SessionStart`: `"session-start"`, `session.hook("prompt"`, `synthetic`, `parentID` e `injected`.
  - `TestPlugin2Package`: es JSON válido con `type=module`, `main=index.js` y la marca.
  - `TestPluginSinDependencias` y los demás de 1.x siguen sin cambios.
- `internal/adapters/agent/opencode/hook_test.go`:
  - `TestParseActionsV2Captura`: recorre `testdata/tool_before_v2.jsonl` (real) con shell, edit, write, patch, read y subagent.
  - `TestParseActionsPathV2`: `path` relativo y absoluto.
  - `TestParseActionsV1SinCambios`: tabla 1.x.
- `internal/adapters/agent/opencode/transcript_test.go`: `TestReadUsageLineaV2` lee `testdata/usage_v2.jsonl`, armado desde `events_v2.jsonl` con la regla de R10.
- `internal/cli/opencode_hooks_test.go`:
  - `TestGuardOpenCodeV2Bloquea`: `shell` y `path` fuera de alcance devuelven 2.
  - `TestRenderPlugin2Conflicto`.
  - `TestRenderPlugin2Stale`: también borra la carpeta.
  - `TestDoctorOpenCodePlugin2`: falta, difiere y ok.
  - `TestHookTokensOpenCodeV2`.

## Tasks

- [x] T1 Contrato (R5, R16). Capturar en OpenCode 2.0.22 `internal/adapters/agent/opencode/testdata/tool_before_v2.jsonl` (`x` de `execute.before` para shell, edit, write, patch, read con offset/limit y subagent) y `events_v2.jsonl` (`session.created` de la raíz y del hijo, y `session.step.started/ended` y `session.execution.succeeded` de los dos). Anotar en el fixture las unidades de `ev.created` y `started`. Crear `adapters/opencode/plugin2/index.js` con la marca y `export default { id: "bflow", setup: async () => () => {} }`, más `package.json`. Agregar `Plugin2` y `Plugin2Package` a `embed.go` y `PluginFiles` a `opencode.go`. Escribir los nombres de prueba de Design con `t.Skip("T<n>")` donde todavía no apliquen. Al terminar, el build y las pruebas pasan.
- [x] T2 Guard en Go (R16). `ParseActions` acepta `shell`, `subagent`, el `patch` de 2.x y `path`. Quitar el skip de `TestParseActionsV2Captura`, `TestParseActionsPathV2`, `TestParseActionsV1SinCambios` y `TestGuardOpenCodeV2Bloquea`.
- [x] T3 Render, doctor y obsoletos (R1, R2, R3, R4). `RenderAgents` y `GeneratedAgents` con las 3 rutas; doctor sobre `PluginFiles`; borrar `bflow2/` vacía. Pruebas `TestRenderIncluyePlugin2`, `TestPlugin2Package`, `TestRenderPlugin2Conflicto`, `TestRenderPlugin2Stale` y `TestDoctorOpenCodePlugin2`.
- [x] T4 Guard del plugin 2.x (R6, R7, R8, R9, R14, R15). `setup` con `resolve`, `run`, `warn`, `ctx.tool.hook("execute.before")` y la función de limpieza. Pruebas `TestPlugin2Guard` y `TestPlugin2SinDependencias` (literales del guard).
- [x] T5 Tokens del plugin 2.x (R10, R11, R12, R14). Iterador de eventos con `session.created`, `step.started/ended` y `execution.succeeded`. Pruebas `TestReadUsageLineaV2`, `TestHookTokensOpenCodeV2` y `TestPlugin2SinDependencias` completo.
- [x] T6 Inyección del estado (R13, R14). `ctx.session.hook("prompt")` con `injected` y `synthetic`. Prueba `TestPlugin2SessionStart`.
- [ ] T7 Docs y validación (R1 a R16). Actualizar `adapters/opencode/README.md` (sección Plugin con los dos destinos), `docs/configuracion.md`, `docs/guia.md`, `docs/instalacion.md` y `CHANGELOG.md`. Correr `bflow render` en el repo para generar `.opencode/plugins/bflow2/`. Validar a mano en Desktop 2.0.22 (el guard bloquea, aparece la caché de tokens, se inyecta el estado) y en CLI 1.1.53 (carga `bflow.js` sin error y con el mismo comportamiento).
