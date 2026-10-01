# Adaptador de OpenCode

`bflow install opencode` instala el comando `/bflow`; los agentes los escribe `bflow render` en `<repo>/.opencode/agents/bflow-<agente>.md` y se commitean. Pon `agent: [claude, opencode]` (o `agent: opencode`) en `bflow.yaml` para generarlos.

| Archivo (embebido en el binario) | Dónde lo deja `bflow install opencode` | Para qué |
|---|---|---|
| `command_head.md` + [`../leader/bflow.md`](../leader/bflow.md) | `$XDG_CONFIG_HOME/opencode/commands/bflow.md`, o `~/.config/opencode/commands/bflow.md` (una vez por máquina) | La sesión principal interpreta `next`; es el mismo cuerpo que la skill de Claude, con la herramienta `question` |
| [`plugin.js`](plugin.js) | `<repo>/.opencode/plugins/bflow.js` (lo escribe `bflow render`) | Guard y medición de tokens |

## Modelos

OpenCode exige `proveedor/modelo`. Los agentes de bflow traen alias (`sonnet`, `haiku`) que se traducen con `models.opencode` en `bflow.yaml`, en un perfil o en la config global (por alias gana el repo, luego el perfil, luego la global):

```yaml
models:
  opencode:
    sonnet: anthropic/claude-sonnet-4
    haiku: anthropic/claude-haiku-4
```

Un valor con `/` se usa tal cual. Un alias sin equivalente deja al agente sin `model:` y `bflow render` y `bflow doctor` lo avisan.

## Plugin: guard y tokens

`bflow render` también escribe `<repo>/.opencode/plugins/bflow.js` (de [`plugin.js`](plugin.js); se commitea y no se edita). Solo le pasa datos a bflow por stdin, sin dependencias de npm:

- `tool.execute.before` de bash, edit, write, multiedit y patch/apply_patch corre `bflow guard --tool opencode`; con exit 2 bloquea con el motivo. Si `bflow` no está en el PATH, tarda más de 10 s o falla, la herramienta pasa y el plugin avisa una vez por sesión.
- `message.updated` agrega cada mensaje de asistente terminado (solo metadatos) a `.bflow/cache/opencode/<sesión>.jsonl`; `session.idle` corre `bflow hook tokens --tool opencode`, que atribuye los tokens por fase, agente y modelo.
- No hay evento de fin de subagente ni de inicio de sesión: el nudge y el resumen de inicio no existen en OpenCode; lo cubren `bflow report`, `bflow check --verify` y el bloque de AGENTS.md.

`bflow doctor` muestra el estado del plugin.
