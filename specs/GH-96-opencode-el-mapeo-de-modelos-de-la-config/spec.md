# GH-96 · OpenCode: el mapeo de modelos de la config global termina en los agentes versionados

## Brief

**Objetivo:** que `.opencode/agents/bflow-*.md` dependa solo de `models.opencode` de `bflow.yaml`, y que el mapeo personal se aplique en la máquina a través del `opencode.json` del usuario.

**Entra**
- `config.Load` separa la tabla del repo (`Config.Models`) de la personal (`Config.PersonalModels`, solo la config global). Lo que defina un perfil en `models` no se aplica y genera un aviso.
- `render` y `render --check` generan los agentes solo con la tabla del repo. Si un alias no está en el repo, el agente sale sin `model:`.
- `render` (sin `--check`) y `bflow install opencode` escriben `agent.bflow-<agente>.model` en `<config de OpenCode>/opencode.json`, solo para los alias que cubre el mapeo personal y no cubre el repo.
- `doctor`: un alias cuenta como sin resolver solo si no lo cubre ninguna de las dos tablas. Avisa si `opencode.json` no se puede editar o no coincide con lo esperado.
- Migración de este repo: un re-render deja los 4 agentes sin `model:`. Docs, CHANGELOG y la nota de GH-13 R3.

**No entra**
- `opencode.json` del proyecto, `opencode.jsonc` (nunca se edita; se muestra el bloque para pegarlo), y tablas de modelos de otras herramientas (`models.claude` sigue siendo error).

**Decisiones nuevas**
- [N] El JSON se reescribe con `encoding/json` (2 espacios, claves ordenadas). Se conserva todo el contenido, pero no el orden ni el formato. Se descartó editar el texto en su lugar porque es frágil. Si no hace falta ningún cambio, no se escribe.
- [N] De `agent.bflow-*` bflow solo toca la clave `model`. Si la entrada queda vacía, se borra. Las demás claves que haya puesto el usuario se quedan.
- [N] La ruta sale de la misma carpeta que ya usa el comando `/bflow` (`$XDG_CONFIG_HOME/opencode` o `~/.config/opencode`, también en Windows). Se descartó `%AppData%` porque OpenCode no la lee.
- [N] `render` reporta la sincronización en `data.personal_models`. Si falla, es un aviso y no un error: los archivos del repo ya se escribieron bien.

**Riesgos**
- `opencode.json` es uno por usuario y los nombres `bflow-*` son iguales en todos los repos. Si dos repos le ponen alias distintos al mismo agente (`agents.<n>.model`), gana el último `render`. `doctor` lo detecta como desincronizado. Quien necesite modelos fijos los pone en `bflow.yaml`.
- Sin verificar: si una entrada `agent.bflow-x` con solo `model` hace aparecer un agente fantasma en repos sin bflow. T6 lo comprueba a mano y lo documenta.
- `go test` no debe tocar el `opencode.json` real: las pruebas fijan `XDG_CONFIG_HOME` y `BFLOW_CONFIG_HOME`.

**Tamaño:** M. 3 paquetes (`config`, `opencode`, `cli`), ~400 líneas con pruebas, más docs.

## Discovery

- Lo versionado depende solo de `bflow.yaml`. La global y los perfiles no entran a los archivos generados, y `--check` los ignora.
- El mapeo personal va a `agent.bflow-<agente>.model` del `opencode.json` de usuario. Lo sincronizan `render` (sin `--check`) e `install opencode`, y se quitan las entradas que sobran.
- Con `.jsonc` o con un JSON que no se puede leer: no se edita, se muestra el bloque y doctor lo reporta. Si no existe, se crea.
- Un perfil con `models.opencode` produce un aviso, no un error. La migración es re-renderizar este repo. Docs: configuracion, guia, README del adaptador y CHANGELOG.

## Requirements

- **R1 [D]** CUANDO `config.Load` combine la configuración, el sistema DEBE dejar en `Config.Models` solo los `models` de `bflow.yaml` y en `Config.PersonalModels` solo los `models` del nivel superior de la config global.
- **R2 [D]** CUANDO el perfil activo defina `models`, el sistema DEBE ignorarlos, poner `Config.ProfileModelsIgnored = true`, y `render` y `doctor` DEBEN avisar que no se aplican (sin fallar la validación).
- **R3 [N]** CUANDO `PersonalModels` traiga una tabla desconocida o un valor vacío, `Validate` DEBE fallar con el mismo mensaje de hoy, nombrando `GlobalPath()` como origen.
- **R4 [D]** CUANDO `render` o `render --check` generen agentes de OpenCode, el sistema DEBE resolver los alias solo con `Config.Models["opencode"]`. Un alias ausente deja el agente sin `model:`, y eso no es error.
- **R5 [D]** CUANDO corra `render --check`, el sistema NO DEBE leer ni escribir el `opencode.json` del usuario, y su resultado NO DEBE depender de la config global ni del perfil.
- **R6 [D]** CUANDO un agente use un alias que el repo no define y la tabla personal sí, `render` (sin `--check`) e `install opencode` DEBEN dejar `agent.<subagente>.model = <valor personal>` en `<config de OpenCode>/opencode.json`.
- **R7 [D]** CUANDO se sincronice `opencode.json`, el sistema DEBE conservar todas las claves que no sean `agent.bflow-*.model`. DEBE quitar `model` de las entradas `bflow-*` que ya no correspondan y borrar las que queden vacías. Si nada cambia, NO DEBE escribir.
- **R8 [D]** CUANDO el alias esté definido en `bflow.yaml` o el modelo del agente ya sea `proveedor/modelo` o esté vacío, el sistema NO DEBE escribir un override para ese agente.
- **R9 [D]** CUANDO no exista `opencode.json` y sí `opencode.jsonc`, o cuando `opencode.json` no se pueda leer como objeto JSON, el sistema NO DEBE editarlo. DEBE dejar en `data.personal_models.paste` el bloque `{"agent": {...}}` que habría que pegar y un aviso en el texto. CUANDO no exista ninguno de los dos y haya overrides, DEBE crear `opencode.json`.
- **R10 [D]** CUANDO `render` o `doctor` listen alias sin resolver, el sistema DEBE contar solo los que no cubren ni el repo ni el mapeo personal.
- **R11 [N]** CUANDO `doctor` revise OpenCode, el sistema DEBE avisar en el área `modelos` si `opencode.json` no se puede editar (caso de R9) o si sus `agent.bflow-*.model` difieren de lo esperado, y sugerir `bflow render`.
- **R12 [N]** CUANDO `render` termine con conflictos, o `opencode` no esté en `agent:`, el sistema NO DEBE tocar `opencode.json`.
- **R13 [N]** CUANDO la sincronización falle por un error de E/S, `render` e `install` DEBEN terminar OK, con el aviso en el texto y en `data.personal_models.error`.
- **R14 [D]** CUANDO se haga un re-render de este repo, los 4 `.opencode/agents/bflow-*.md` DEBEN quedar sin `model:` y `render --check` DEBE pasar sin config global.
- **R15 [D]** La documentación (`docs/configuracion.md`, `docs/guia.md`, `adapters/opencode/README.md`), el CHANGELOG y el aviso de `init` (`setupcmds.go:367`) DEBEN describir el comportamiento nuevo. `specs/GH-13-*/spec.md` R3 DEBE llevar una nota de que GH-96 lo reemplaza.

## Design

### config (`internal/config/config.go`)
- `Config` gana dos campos: `PersonalModels map[string]map[string]string \`yaml:"-"\`` y `ProfileModelsIgnored bool \`yaml:"-"\``.
- `Load`: se borra la línea que hoy copia `globalMap["models"]` a `merged`. `PersonalModels` se decodifica de `globalMap["models"]`. Antes de `deepMerge` con el perfil, si `pm["models"]` existe, se borra de la copia y se marca `ProfileModelsIgnored`. Así `Config.Models` queda solo con lo de `repoMap`.
- `Validate`: el bucle de modelos pasa a `validateModels(prefix string, m map[...]...)`, que se llama para `c.Models` (prefijo `models`) y para `c.PersonalModels` (prefijo `models` y sufijo ` (en <GlobalPath()>)`).

### opencode (`internal/adapters/agent/opencode/models.go`, nuevo)
- `configDir(home string) (string, error)`: se saca de `commandPath`, que pasa a ser `configDir + "commands/bflow.md"`. Mantiene la misma validación de `XDG_CONFIG_HOME`.
- `const userConfig = "opencode.json"`.
- ```go
  // ModelSync es el resultado de llevar los overrides a opencode.json.
  type ModelSync struct {
      Path    string            // opencode.json (o .jsonc si es el que existe)
      Changed bool
      Paste   string            // bloque {"agent":{...}} si no se pudo editar; "" si se editó
      Want    map[string]string // subagente → proveedor/modelo
  }
  func (Agent) SyncModels(home string, want map[string]string) (ModelSync, error)
  func (Agent) ModelsState(home string, want map[string]string) (ModelSync, string) // estado: ok|stale|manual
  ```
- `SyncModels` lee el archivo como `map[string]any` y lo modifica según R7. Escribe con `json.MarshalIndent(_, "", "  ")` + `\n` vía `writeAtomic`. Si `want` está vacío y el archivo no existe, no crea nada.
- `cli` declara una interfaz opcional `ModelOverrider` (en `cli.go`, junto a `LogDoctor`) con esos dos métodos. Solo el adaptador de OpenCode la implementa.

### cli
- `renderPlan` gana `Overrides map[string]map[string]string` (herramienta → subagente → modelo). `planRender` resuelve con la tabla del repo. Por cada `Unresolved`, si `cfg.PersonalModels[tool][alias]` existe, cada agente de `u.Agents` va a `Overrides` y el alias sale de `Unresolved`. Si no existe, se queda en `Unresolved`. `planRender` no escribe nada.
- `syncPersonalModels(cfg, p) map[string]any`: llama a `SyncModels` por cada herramienta que implementa `ModelOverrider` y está en `cfg.Agent.Rendered()`. Lo llaman `runRender` (sin `--check`, después de `writeRender`) y `runInstall` (después de `applyRender`, si no hay conflictos). Devuelve `{path, changed, agents: [..], paste, error}`, que va a `data.personal_models`. Con `--check` el valor es `null`.
- El texto de `render` agrega, cuando aplica: `modelos personales: <path> (actualizado|sin cambios)`, `pega en <path>: <bloque>` o `aviso: los models del perfil <p> no se aplican; ponlos en bflow.yaml o en <GlobalPath()>`.
- `doctor` (`setupcmds.go:~790`): con `Unresolved` ya filtrado, el texto cambia a "define models.opencode.<alias> en bflow.yaml (equipo) o en <GlobalPath()> (solo tú)". `ModelsState` agrega `modelos warn` cuando el estado es `stale` o `manual`. Avisa también si `ProfileModelsIgnored`.

### Descartado
- Seguir resolviendo la global en memoria y excluirla solo en `--check`: los archivos seguirían cambiando entre máquinas.
- Escribir en el `opencode.json` del proyecto: también se versiona.
- Un `opencode.json` aparte para bflow (`OPENCODE_CONFIG`): obliga a cambiar el entorno de cada sesión.

### Seguridad
- Solo se escribe dentro de `configDir(home)`. Se rechaza una `XDG_CONFIG_HOME` relativa, igual que hoy, y la escritura es atómica con permisos 0644.
- Los valores de modelo son texto de config local. Van al JSON vía `encoding/json` y nunca se interpolan.
- Un JSON que no se puede leer no se sobrescribe (R9), para no perder la config del usuario. No se leen ni se muestran otras claves, que pueden traer secretos de proveedores. `paste` solo lleva el bloque `agent` de bflow.
- No hay datos sensibles nuevos ni cambios de authz.

## Tasks

- [ ] **T1 Contrato.** Firmas en stub: `Config.PersonalModels`, `Config.ProfileModelsIgnored`, `opencode.ModelSync`, `Agent.SyncModels`, `Agent.ModelsState`, `cli.ModelOverrider`, `renderPlan.Overrides`. Pruebas, que fallan hasta T2–T5:
  - `config`: `TestModelsRepoOnly` (R1), `TestModelsProfileIgnored` (R2), `TestModelsValidate` ampliada con la global (R3). `TestModelsMerge` se reescribe a la semántica nueva.
  - `opencode`: `TestSyncModelsCreate` (R6, R9), `TestSyncModelsPreserve` (R7), `TestSyncModelsNoop` (R7), `TestSyncModelsJSONC` (R9), `TestSyncModelsBadJSON` (R9), `TestModelsState` (R11).
  - `cli`: `TestRenderIgnoresGlobalModels` (R4, R5), `TestRenderSyncsPersonalModels` (R6, R8, R10), `TestRenderCheckNoUserConfig` (R5), `TestRenderConflictNoSync` (R12), `TestInstallOpenCodeSyncsModels` (R6), `TestDoctorOpenCodeModels` (R10, R11, R2).
  - e2e: `cmd/bflow/e2e_render_test.go: TestRenderAgentsGlobalModelsNotVersioned` (R4, R5).
  - Todas fijan `XDG_CONFIG_HOME` y `BFLOW_CONFIG_HOME` a `t.TempDir()`.
- [ ] **T2 config.** Separar las tablas en `Load`, el aviso del perfil y `validateModels` (R1, R2, R3).
- [ ] **T3 opencode.json.** `configDir`, `SyncModels` y `ModelsState` en `models.go` (R6, R7, R9).
- [ ] **T4 render, install y check.** `planRender` con `Overrides` y `Unresolved` filtrado, `syncPersonalModels` en `runRender` y `runInstall`, `data.personal_models` y los textos (R4–R6, R8, R10, R12, R13).
- [ ] **T5 doctor e init.** El área `modelos` con `ModelsState`, el aviso del perfil, y los textos de `setupcmds.go:367` y `:796` (R2, R11, R15).
- [ ] **T6 Migración y docs.** Re-render de este repo (los 4 agentes sin `model:`), `render --check` en verde con `BFLOW_CONFIG_HOME` vacío y `go install` desde la rama antes de quality. Comprobar a mano en OpenCode que el override se aplica y si aparecen agentes fantasma en un repo sin bflow, y anotar el resultado en el README del adaptador. Actualizar `docs/configuracion.md`, `docs/guia.md`, `adapters/opencode/README.md`, el CHANGELOG y la nota en GH-13 R3 (R14, R15).
