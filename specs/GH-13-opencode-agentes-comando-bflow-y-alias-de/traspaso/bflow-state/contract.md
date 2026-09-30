# Contrato GH-13: OpenCode (agentes, comando /bflow, alias de modelos)

Tipos y firmas (stubs sin lógica; compilan y las pruebas nuevas fallan contra ellos):
- config: `type Tools []string` (UnmarshalYAML, Has, Rendered), `Config.Agent Tools`, `Config.Models map[string]map[string]string`, `globalFile.Models`, `Known.Agents = claude, opencode`, `Known.ModelTables = opencode`.
- agents: `const GeneratedMark` (movida desde claude), `type Unresolved{Alias, Agents}`, `ResolveModels(specs, table) ([]Spec, []Unresolved)`.
- cli: `ToolAdapter` (Name, RenderAgents, GeneratedAgents, InstallSkill, SkillState, Version, ResolvesModels); `AgentAdapter` lo embebe; `Env.Tools`, `Ctx.tools()`, `Ctx.tool(name)`, `renderPlan.Unresolved`.
- setup: `Profile.Agent` y `Answers.Agent` pasan a `[]string`.
- adapters/leader: `Render(head, vars)` y `bflow.md` (cuerpo común con `{{ask_tool}}`); adapters/claude: `skill_head.md`; adapters/opencode: `Command`, `command_head.md`.
- internal/adapters/agent/opencode: `Agent` (registrado en cmd/bflow `Tools`).
- Decisión: `ask_tool` de OpenCode = "la herramienta `question`"; el de Claude = "AskUserQuestion".

Pruebas (cada una con cuerpo real; hoy fallan):
- config: TestToolsYAML (R1 texto/lista/vacío/ausente, error con otro tipo, Rendered nil -> claude, Has); TestAgentListValidate (R2 desconocido y repetido con mensaje); TestModelsMerge (R3 global < perfil < repo por alias); TestModelsValidate (R4 tabla desconocida, alias vacío, valor no texto, ruta del archivo).
- agents: TestResolveModels (R6 "/" tal cual, alias traducido, vacío, ausente -> Unresolved ordenado, sin mutar).
- adapters: TestLeaderRender (reemplazo, marcador sin valor, variable sobrante); TestSkillFromCommonBody (R11/R12 skill = head+cuerpo y sha256 de antes); TestCommandFromCommonBody (R11/R13 comando, !`bflow status`, sin marcadores ni AskUserQuestion, acciones y secciones).
- adaptador opencode: TestOpenCodeRender (R5 frontmatter, permisos, sin model, sin inyección de claves); TestOpenCodeGenerated (R8 solo con marca); TestOpenCodeInstallXDG (R14); TestOpenCodeInstallBadDir (R16); TestOpenCodeSkillStateCRLF (R21).
- cli: TestRenderMultiTool (R5,R7,R10); TestRenderStaleInactiveTool (R8); TestRenderUnresolved (R6 data.unresolved y `aviso:`); TestRenderEmptyAgentClaudeOnly (R9, guarda de regresión: ya pasa); TestInstallOpenCode y TestInstallOpenCodeNotInAgent (R14-R16); TestInstallOpencodeTool (antes "unsupported", ahora espera `installed`); TestUpdateRefreshesIntegrations (R17); TestInitDetectsOpenCode (R18,R20); TestInitAgentFlagList (R19); TestDoctorOpenCode y TestDoctorOpenCodeOnly (R21,R22).
- setup: TestRenderYAMLAgentList (R18 escalar o lista en línea, omite si igual al perfil).
- Sin prueba: R23 (docs, T8).
