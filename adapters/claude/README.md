# Adaptador de Claude Code

La skill y los hooks se copian a mano. Los agentes no: `bflow render` los escribe en `<repo>/.claude/agents/bflow-<agente>.md` y se commitean.

| Archivo | Dónde va | Para qué |
|---|---|---|
| `skills/bflow/SKILL.md` | `~/.claude/skills/bflow/` (una vez por máquina) | La sesión principal interpreta `next`; no contiene reglas del flujo |
| `settings.json` | `<repo>/.claude/settings.json` (combinar si ya existe) | Hooks: `bflow hook session-start`, `bflow guard` antes de Bash/Edit/Write, `bflow hook tokens` al terminar turnos y subagentes; barra de estado `bflow statusline` |

Los hooks van por repo y no globales: `bflow guard` aplica reglas (ramas protegidas, `.env`, pruebas congeladas) que solo tienen sentido donde hay `bflow.yaml`.

`bflow doctor` avisa si el repo declara `agent: claude` y faltan los hooks.
