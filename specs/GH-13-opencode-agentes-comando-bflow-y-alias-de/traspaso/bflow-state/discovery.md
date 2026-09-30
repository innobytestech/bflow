# Discovery GH-13: OpenCode: agentes, comando /bflow y alias de modelos

## Contexto (leído del repo)
- Todo está atado a Claude: cmd/bflow/main.go:29 inyecta siempre claude.Agent{}; `Config.Agent` es string con Known.Agents = {"", "claude"} (config.go:32,174,498).
- Existe la interfaz cli.AgentAdapter (cli.go:182-212). Render: agents.Build -> Agent.RenderAgents -> diff contra GeneratedAgents; borra lo stale y actualiza el bloque de AGENTS.md si existe (rendercmds.go).
- Spec neutral en internal/agents/agents.go:28 (Tools read/write/bash, Model, Effort, OmitClaudeMd). Defaults: sonnet (implementer, reviewer, security-auditor, ux-auditor), haiku (documenter), vacío (spec-author, ui-designer).
- install opencode hoy falla con install_unsupported (installcmds.go:29, test install_test.go:169). update refresca con `install claude --skill-only` fijo (updatecmds.go:77).
- Chequeos `== "claude"` en installcmds.go:82, setupcmds.go:267,333,557, updatecmds.go:77.
- Perfil global: GlobalDir()/config.yaml, decodificado estricto (globalFile en config.go:165); los perfiles se fusionan por debajo de bflow.yaml.
- Skill: adapters/claude/skills/bflow/SKILL.md, embebida en adapters/claude/embed.go; usa !`bflow status $ARGUMENTS --json` y AskUserQuestion (líneas 18 y 33).
- archtest: solo cmd/bflow puede importar internal/adapters/...; un internal/adapters/agent/opencode es válido.

## Alcance decidido
1. `agent:` en bflow.yaml acepta texto o lista: `agent: claude` sigue valiendo; `agent: [claude, opencode]` activa ambas. Valores válidos: claude, opencode. Duplicados o desconocidos: error que lista las disponibles.
2. Adaptadores: cmd/bflow registra claude y opencode; render, install, doctor e init recorren las herramientas de `agent:`. Los hooks y el guard siguen siendo de Claude (el plugin de OpenCode es GH-14).
3. Render para opencode escribe .opencode/agents/bflow-*.md con frontmatter `description`, `mode: subagent`, `model` (alias resuelto; se omite si no resuelve) y permisos equivalentes a read/write/bash. Effort y OmitClaudeMd no aplican. Lleva la misma marca de generado; lo stale se borra solo dentro de .opencode/agents/bflow-*.md. Si opencode sale de `agent:`, sus archivos quedan como stale y se borran igual que en Claude.
4. Comando /bflow global: `bflow install opencode` escribe ~/.config/opencode/commands/bflow.md (respeta XDG_CONFIG_HOME). Inyecta !`bflow status $ARGUMENTS --json`. Dentro de un repo con bflow.yaml corre render si `agent:` incluye opencode; si no lo incluye, instala el comando y avisa. Sin settings que fusionar (eso es GH-14). `--skill-only` escribe solo el comando.
5. Una sola fuente para skill y comando: un cuerpo común con marcadores (nombre de la herramienta de preguntas: AskUserQuestion / question, más lo que difiera); cada adaptador pone su frontmatter. Un test asegura que ambos salen del mismo cuerpo y que no queda ningún marcador sin reemplazar.
6. `bflow update` refresca cada integración ya instalada (skill de Claude, comando de OpenCode) con el binario nuevo; si una falla, update sigue OK y dice cómo refrescar.
7. Alias de modelos: tabla `models.opencode` (alias -> proveedor/modelo) en el perfil global, que bflow.yaml puede fijar para todo el repo (el repo gana). Un valor que ya trae "/" se usa tal cual. Sin defaults incluidos en el binario. Si un alias no resuelve se omite `model:` (hereda el del agente principal) y doctor avisa. Claude sigue usando el alias directo.
8. init sin --agent: agrega opencode si existe .opencode/, opencode.json u opencode.jsonc; agrega claude con las señales de hoy (.claude/ o CLAUDE.md). Si hay las dos, `agent: [claude, opencode]`. El consejo "siguiente" de init cubre cada herramienta.
9. doctor con opencode en `agent:`: binario opencode presente (muestra versión, sin mínimo); comando global presente y al día (normalizando CRLF), si no sugiere `bflow install opencode`; agentes renderizados al día; cada alias usado resuelve. Informa que guard y tokens de OpenCode llegan con GH-14.
10. Documentación: README, docs/guia.md y un adapters/opencode/README.md cuentan cómo instalar y configurar los alias; CHANGELOG.

## Restricciones
- Fuente de verdad en adapters/ (embebida en el binario), sin copias duplicadas.
- Salida para agentes mínima: JSON compacto. Sin emojis en texto para personas.
- Respetar archtest. Compatibilidad: repos con `agent: claude` o sin `agent:` se comportan igual que hoy.

## Errores
- `agent:` con valor desconocido o duplicado: error con las disponibles.
- Carpeta de configuración de OpenCode no resoluble o sin permiso: falla con ruta y motivo.
- Alias no resuelto: no es error; render omite `model:` y doctor avisa.
- Perfil global con `models` mal formado: error de decodificación con la ruta del archivo.

## Fuera de alcance
- Plugin de OpenCode con guard, hooks y medición de tokens (GH-14).
- Versión mínima de OpenCode.
- Defaults de alias incluidos en el binario.
