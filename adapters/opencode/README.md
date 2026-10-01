# Adaptador de OpenCode

`bflow install opencode` instala el comando `/bflow`; los agentes los escribe `bflow render` en `<repo>/.opencode/agents/bflow-<agente>.md` y se commitean. Pon `agent: [claude, opencode]` (o `agent: opencode`) en `bflow.yaml` para generarlos.

| Archivo (embebido en el binario) | Dónde lo deja `bflow install opencode` | Para qué |
|---|---|---|
| `command_head.md` + [`../leader/bflow.md`](../leader/bflow.md) | `$XDG_CONFIG_HOME/opencode/commands/bflow.md`, o `~/.config/opencode/commands/bflow.md` (una vez por máquina) | La sesión principal interpreta `next`; es el mismo cuerpo que la skill de Claude, con la herramienta `question` |

## Modelos

OpenCode exige `proveedor/modelo`. Los agentes de bflow traen alias (`sonnet`, `haiku`) que se traducen con `models.opencode` en `bflow.yaml`, en un perfil o en la config global (por alias gana el repo, luego el perfil, luego la global):

```yaml
models:
  opencode:
    sonnet: anthropic/claude-sonnet-4
    haiku: anthropic/claude-haiku-4
```

Un valor con `/` se usa tal cual. Un alias sin equivalente deja al agente sin `model:` y `bflow render` y `bflow doctor` lo avisan.

## Pendiente (GH-14)

Los hooks, el guard y el conteo de tokens de bflow siguen siendo de Claude Code; `bflow doctor` lo recuerda. Hasta GH-14, en OpenCode las reglas del flujo dependen del contrato de cada agente.
