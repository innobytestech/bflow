# GH-13 · OpenCode: agentes, comando /bflow y alias de modelos

## Brief

**Objetivo:** que un repo con `agent: [claude, opencode]` (o solo `opencode`) tenga en OpenCode los mismos agentes `bflow-*`, un comando `/bflow` equivalente a la skill y modelos resueltos con alias propios.

**Entra**
- `agent:` acepta texto o lista (claude, opencode); `agent: claude` y la ausencia de `agent:` se comportan igual que hoy.
- `bflow render` genera también `.opencode/agents/bflow-*.md` (`mode: subagent`) y borra los de una herramienta que salió de `agent:`.
- `bflow install opencode`: comando global `/bflow` en `~/.config/opencode/commands/bflow.md` (respeta XDG_CONFIG_HOME) y render si aplica.
- Skill de Claude y comando de OpenCode salen de un solo cuerpo con marcadores (AskUserQuestion / question).
- Tabla `models.opencode` (alias -> proveedor/modelo) en la config global; bflow.yaml o el perfil pueden fijar alias (el repo gana).
- `bflow update` refresca cada integración instalada; `init` detecta OpenCode; `doctor` revisa binario, comando, agentes y alias.
- README, docs/guia.md, adapters/opencode/README.md.

**No entra:** plugin de OpenCode con guard, hooks y tokens (GH-14); versión mínima de OpenCode; alias por defecto dentro del binario.

**Decisiones nuevas**
- [N1] Interfaz nueva `cli.ToolAdapter` (render, install, estado, versión) que `AgentAdapter` incluye; OpenCode implementa solo esa. Descartado: que OpenCode implemente todo `AgentAdapter` con hooks y transcripts vacíos.
- [N2] `models:` vale en el nivel superior de la config global (como `ui`, es de la persona) y también en perfiles y bflow.yaml, fusionado por alias. Descartado: solo dentro de un perfil (obliga a usar perfil para tener alias).
- [N3] Solo `models.opencode` es válido; `models.claude` u otra clave es error. Descartado: aceptar y aplicar tablas a Claude.
- [N4] Sin `agent:`, render sigue generando solo los de Claude (como hoy). Descartado: no generar nada.
- [N5] Permisos del agente OpenCode con `permission: {edit, bash}` = allow/deny; leer siempre está permitido. Descartado: `tools:` booleano (obsoleto en OpenCode).
- [N6] La skill de Claude que sale del cuerpo común es idéntica byte a byte a la SKILL.md actual, para que quien actualiza no la vea como desactualizada.
- [N7] doctor marca como `warn` que guard y tokens no cubren OpenCode hasta GH-14. Descartado: `ok` informativo (esconde que el guard no protege esas sesiones).
- [N8] `init --agent claude,opencode` (separado por comas). `update` agrega `data.integrations` y conserva `data.skill` (Claude).
- [N9] La marca de generado pasa a `agents.GeneratedMark`, compartida por los dos adaptadores.

**Riesgos**
- El formato de agentes de OpenCode cambia sin versión mínima: si ignora `permission`, los agentes quedan con permisos por defecto. Mitiga: doctor muestra la versión; README documenta el formato usado.
- Con `agent: [opencode]`, render borra los `.claude/agents/bflow-*` generados (nunca los que no llevan la marca).
- `install opencode` sobrescribe un `/bflow` propio del usuario, igual que la skill de Claude hoy.

**Tamaño:** L. 8 tareas; toca config, agents, cli (render, install, update, init, doctor), un adaptador nuevo y la fuente de la skill.

## Discovery

Todo está atado a Claude: main.go inyecta `claude.Agent{}` y `Config.Agent` es un texto con solo "claude". Existe `cli.AgentAdapter` y un render neutral (agents.Spec -> adaptador). El humano decidió: `agent:` como texto o lista, adaptador OpenCode para render/install/doctor/init/update (hooks y guard siguen siendo de Claude, GH-14), comando `/bflow` global, cuerpo común skill/comando, tabla de alias `models.opencode` sin defaults, detección por `.opencode/`, `opencode.json(c)`. Discovery completo en el tracker.

## Requirements

Configuración
- R1 [D] CUANDO bflow.yaml, un perfil o la config global traen `agent:` como texto (`agent: claude`) o como lista (`agent: [claude, opencode]`), el sistema DEBE cargarlo como lista de herramientas; `agent: claude`, `agent: ""` y la ausencia de `agent:` DEBEN producir el mismo comportamiento que hoy en render, install, init y doctor.
- R2 [D] CUANDO `agent:` trae un valor que no es claude u opencode, o repite uno, `config.Load` DEBE fallar con un mensaje que nombra el valor y lista las disponibles (`claude, opencode`).
- R3 [D] CUANDO la config global trae `models.opencode` en el nivel superior, el sistema DEBE usarla; un perfil y después bflow.yaml DEBEN poder fijar alias sueltos, ganando por alias (el repo gana).
- R4 [N] CUANDO `models` trae una clave distinta de `opencode`, un alias con valor vacío o un valor que no es texto, `config.Load` DEBE fallar con la ruta del archivo que lo trae (o "configuración combinada" si sale de la fusión).

Render
- R5 [D] CUANDO `agent:` incluye opencode, `bflow render` DEBE escribir `.opencode/agents/<subagent>.md` por cada agente del flujo, con frontmatter `description`, `mode: subagent`, `model` (solo si resuelve) y `permission` (`edit`: allow si el agente tiene write, si no deny; `bash`: allow si tiene bash, si no deny), seguido de `agents.GeneratedMark` y el mismo cuerpo que el agente de Claude.
- R6 [D] Al resolver el modelo para OpenCode, un valor que contiene "/" DEBE usarse tal cual; un alias presente en `models.opencode` DEBE cambiarse por su valor; un modelo vacío DEBE omitir `model:`; un alias ausente DEBE omitir `model:` y aparecer en `data.unresolved` (`[{"tool","alias","agents"}]`) y en una línea `aviso:` del texto de render.
- R7 [D] Los agentes de Claude DEBEN seguir usando el alias directo; su contenido no cambia por esta tarea.
- R8 [D] CUANDO una herramienta registrada no está entre las que se generan, sus archivos generados (con la marca) DEBEN salir como `stale` y borrarse; un archivo sin la marca nunca se borra, y uno sin la marca en una ruta que render escribiría DEBE seguir dando `render_conflict`.
- R9 [N] CUANDO `agent:` está vacío, render DEBE generar solo los agentes de Claude.
- R10 [D] `bflow render --check` DEBE cubrir los archivos de todas las herramientas generadas.

Skill y comando
- R11 [D] La skill de Claude y el comando `/bflow` de OpenCode DEBEN salir del mismo cuerpo embebido; la de Claude nombra AskUserQuestion y el de OpenCode la herramienta `question`; ninguno DEBE conservar un marcador `{{...}}` sin reemplazar ni el nombre de la herramienta de preguntas de la otra.
- R12 [N] La skill de Claude generada DEBE ser idéntica byte a byte a `adapters/claude/skills/bflow/SKILL.md` antes de esta tarea.
- R13 [D] El comando de OpenCode DEBE llevar frontmatter con `description` e inyectar `` !`bflow status $ARGUMENTS --json` ``.

Install y update
- R14 [D] `bflow install opencode` DEBE escribir el comando en `$XDG_CONFIG_HOME/opencode/commands/bflow.md`, o `~/.config/opencode/commands/bflow.md` si la variable está vacía, y responder `installed` con `data.skill` = esa ruta y `data.settings` = "".
- R15 [D] Con `--skill-only` DEBE escribir solo el comando. Sin bflow.yaml DEBE avisar que los agentes se generan por repo. En un repo cuyo `agent:` incluye opencode DEBE correr render (`data.render: true`); si no lo incluye DEBE avisar cómo agregarlo y no renderizar.
- R16 [D] CUANDO la carpeta de configuración no se resuelve (XDG_CONFIG_HOME relativa, sin home) o no se puede escribir, install DEBE fallar con código `install` y un mensaje con la ruta y el motivo.
- R17 [D] CUANDO `bflow update` reemplaza el binario, DEBE correr `install <herramienta> --skill-only --json` por cada herramienta cuya skill o comando ya estaba instalado; una falla DEBE dejar el update en `updated` y decir en el texto `corre bflow install <herramienta>`. `data.integrations` DEBE mapear herramienta -> updated|failed|skipped y `data.skill` DEBE conservar el valor de Claude.

Init y doctor
- R18 [D] CUANDO `init` corre sin `--agent` ni agente en el perfil, DEBE agregar claude si existe `.claude/` o `CLAUDE.md` y opencode si existe `.opencode/`, `opencode.json` u `opencode.jsonc`; con las dos DEBE escribir `agent: [claude, opencode]`, con una `agent: <nombre>`.
- R19 [N] `init --agent` DEBE aceptar nombres separados por comas; un nombre desconocido o repetido DEBE fallar con código `usage` antes de escribir bflow.yaml.
- R20 [D] El texto "siguiente" de init DEBE traer una línea por herramienta: la de hoy para claude y, para opencode, `bflow install opencode` más dónde definir `models.opencode`.
- R21 [D] CUANDO `agent:` incluye opencode, doctor DEBE reportar: versión de `opencode --version` (warn si no corre), estado del comando `/bflow` (ok, o warn con `bflow install opencode` si falta o difiere, normalizando CRLF), alias sin resolver (warn con los agentes afectados y la ruta de la config global) y un warn de que guard y tokens de OpenCode llegan con GH-14.
- R22 [N] El chequeo `agentes` de doctor DEBE correr una sola vez cuando `agent:` no está vacío y cubrir todas las herramientas; los demás chequeos de Claude siguen solo con claude en `agent:`.

Documentación
- R23 [D] README, docs/guia.md y adapters/opencode/README.md DEBEN explicar `agent:` como lista, `bflow install opencode` y cómo configurar `models.opencode`, con el aviso de GH-14.

## Design

### Configuración (internal/config)
```go
// Tools son las herramientas de agente del repo. En YAML: texto o lista.
type Tools []string
func (t *Tools) UnmarshalYAML(n *yaml.Node) error // escalar -> [v] ("" -> nil); secuencia de escalares -> lista; otro -> error
func (t Tools) Has(name string) bool
func (t Tools) Rendered() []string                // nil -> ["claude"] (R9)

type Config struct { ...; Agent Tools `yaml:"agent,omitempty"`; Models map[string]map[string]string `yaml:"models,omitempty"`; ... }
type globalFile struct { Profiles map[string]Config; UI UI; Models map[string]map[string]string `yaml:"models"` }
var Known = struct{ Trackers, Hosts, Agents, ModelTables []string }{..., Agents: {"claude", "opencode"}, ModelTables: {"opencode"}}
```
- `Load`: `models` de la config global entra a `merged` igual que `ui` (antes del perfil); deepMerge fusiona por alias.
- `Validate`: desconocido `agent %q no existe (disponibles: claude, opencode)`; repetido `agent repite %q`; `models.%s no existe (disponible: opencode)`; `models.opencode.%s está vacío`.
- setup: `Profile.Agent` y `Answers.Agent` pasan a `[]string`; `RenderYAML` emite escalar con uno y lista en estilo flujo (`[claude, opencode]`) con varios; se omite si es igual al del perfil.

### Agentes (internal/agents)
```go
const GeneratedMark = "<!-- generado por bflow render: ... -->" // mismo texto de hoy (se mueve desde claude)
type Unresolved struct{ Alias string; Agents []string } // Agents = Subagent, ordenados
func ResolveModels(specs []Spec, table map[string]string) ([]Spec, []Unresolved) // no muta specs; Unresolved ordenado por Alias
```

### CLI (internal/cli)
```go
type ToolAdapter interface {
	Name() string
	RenderAgents(specs []agents.Spec) (map[string][]byte, error)
	GeneratedAgents(root string) []string
	InstallSkill(home string) (path string, err error) // skill (Claude) o comando /bflow (OpenCode)
	SkillState(home string) (path, state string)        // ok | missing | stale
	Version() (have, min string, ok bool, err error)    // min "" = sin mínimo
	ResolvesModels() bool                                // true: model debe ser proveedor/modelo (models.<Name()>)
}
type AgentAdapter interface { ToolAdapter; ParsePreToolUse(...); TokenSource(...); ... } // resto igual
type Env struct { ...; Agent AgentAdapter; Tools []ToolAdapter } // Tools nil -> [Agent] si Agent != nil
func (c *Ctx) tools() []ToolAdapter
func (c *Ctx) tool(name string) ToolAdapter // nil si no está registrada
```
- `renderPlan` suma `Unresolved map[string][]agents.Unresolved` (por herramienta). `planRender`: `agents.Build` una vez; por cada herramienta de `cfg.Agent.Rendered()` que esté registrada: resuelve modelos si `ResolvesModels()` con `cfg.Models[Name()]`, renderiza y fusiona `Files`; `GeneratedAgents` se consulta en todas las registradas, así lo de una herramienta inactiva queda en `Stale`. Una herramienta de `agent:` no registrada es error `no hay adaptador de <x>`.
- `install`: `claude` y `opencode` comparten `runInstall` vía `c.tool(name)`; `InstallSettings` solo con claude. Aviso sin `agent:` correcto: `agent: no incluye <x> en bflow.yaml, así que no se generaron sus agentes; agrégalo (agent: [claude, opencode]) y corre bflow render`.
- `update`: recorre `c.tools()` (orden claude, opencode).
- `init`: `usesOpenCode(root)`; `--agent` se parte por comas y se valida contra `config.Known.Agents`.
- `doctor`: bloque claude sin cambios salvo que `agentes` sale del bloque (R22); bloque opencode nuevo con áreas `agente`, `comando`, `modelos`, `guard`.

### Adaptador OpenCode
- `adapters/leader/` (paquete `leader`): `bflow.md` (cuerpo común, hoy el de SKILL.md sin frontmatter, con `{{ask_tool}}`) y `func Render(head []byte, vars map[string]string) ([]byte, error)`: concatena head + cuerpo, reemplaza, falla si queda `{{[a-z_]+}}` o sobra una variable.
- `adapters/claude/embed.go`: `skill_head.md` (frontmatter actual) + `Skill = must(leader.Render(head, {"ask_tool": "AskUserQuestion"}))`; se borra `skills/bflow/SKILL.md`.
- `adapters/opencode/` (paquete `opencode`): `embed.go` (`Command`), `command_head.md`, `README.md`, `adapter_test.go`.
- `internal/adapters/agent/opencode`: `type Agent struct{}`; `agentsDir = ".opencode/agents"`; `frontmatter{Description, Mode, Model omitempty, Permission{Edit, Bash}}`; `configDir(home) (string, error)`; escritura atómica propia (temp + rename); `Version` corre `opencode --version` con 10 s de límite. Solo `cmd/bflow` lo importa (archtest).
- `cmd/bflow/main.go`: `Tools: []cli.ToolAdapter{claude.Agent{}, opencode.Agent{}}`.

### Seguridad
- Escritura solo en `.opencode/agents/bflow-*.md` del repo y en el comando del usuario; el borrado exige la marca de generado (R8).
- `XDG_CONFIG_HOME` debe ser absoluta; si no, error (R16). Sin secretos nuevos: `models` pasa por `findSecrets` como todo el YAML.
- Los valores de `models` van al frontmatter con `yaml.Marshal`, sin concatenar texto (sin inyección de claves).
- Hooks y guard no cambian: siguen siendo de Claude; doctor lo avisa (R21).

## Tasks

- [ ] T1 Contrato (R1-R23): tipos y firmas del Design sin lógica (`config.Tools`, `Config.Models`, `agents.GeneratedMark`, `agents.ResolveModels`, `cli.ToolAdapter`, `Env.Tools`, paquetes `leader`, `adapters/opencode`, `internal/adapters/agent/opencode` con stubs) y pruebas que fallan: `TestToolsYAML`, `TestAgentListValidate`, `TestModelsMerge`, `TestModelsValidate` (config); `TestResolveModels` (agents); `TestLeaderRender`, `TestSkillFromCommonBody`, `TestCommandFromCommonBody` (adapters); `TestOpenCodeRender`, `TestOpenCodeGenerated`, `TestOpenCodeInstallXDG`, `TestOpenCodeInstallBadDir`, `TestOpenCodeSkillStateCRLF` (adaptador); `TestRenderMultiTool`, `TestRenderStaleInactiveTool`, `TestRenderUnresolved`, `TestRenderEmptyAgentClaudeOnly`, `TestInstallOpenCode`, `TestInstallOpenCodeNotInAgent`, `TestUpdateRefreshesIntegrations`, `TestInitDetectsOpenCode`, `TestInitAgentFlagList`, `TestDoctorOpenCode`, `TestRenderYAMLAgentList` (cli/setup). `install_test.go:169` pasa a esperar `installed`.
- [ ] T2 Config (R1-R4): `Tools` con texto/lista, `Models` en Config y globalFile, fusión como `ui`, validaciones; usos de `cfg.Agent` como texto pasan a `Has`.
- [ ] T3 Cuerpo común (R11-R13): paquete `leader`, skill de Claude desde él (verificar una vez contra `git show HEAD:adapters/claude/skills/bflow/SKILL.md`), comando de OpenCode; `adapters/claude/adapter_test.go` lee `files.Skill`; `adapters/opencode/adapter_test.go` repite los chequeos de acciones y comandos existentes.
- [ ] T4 Adaptador OpenCode y alias (R5, R6, R14, R16): `agents.ResolveModels`, `GeneratedMark` compartida, `internal/adapters/agent/opencode` completo.
- [ ] T5 Render multi-herramienta (R5-R10): `ToolAdapter`, `Env.Tools` con fallback, `planRender`/`runRender` con `unresolved`, registro en `cmd/bflow`.
- [ ] T6 Install y update (R14-R17): `install opencode`, aviso por herramienta, `data.integrations`.
- [ ] T7 Init y doctor (R18-R22): detección, `--agent` con comas, `RenderYAML` con lista, "siguiente" por herramienta, bloque opencode y `agentes` común.
- [ ] T8 Documentación (R23): README, docs/guia.md, adapters/opencode/README.md (el CHANGELOG lo escribe el documenter).
