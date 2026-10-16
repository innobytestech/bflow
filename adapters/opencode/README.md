# Adaptador de OpenCode

`bflow install opencode` instala el comando `/bflow`; los agentes los escribe `bflow render` en `<repo>/.opencode/agents/bflow-<agente>.md` y se commitean. Pon `agent: [claude, opencode]` (o `agent: opencode`) en `bflow.yaml` para generarlos.

| Archivo (embebido en el binario) | Dónde lo deja `bflow install opencode` | Para qué |
|---|---|---|
| `command_head.md` + [`../leader/bflow.md`](../leader/bflow.md) | `$XDG_CONFIG_HOME/opencode/commands/bflow.md`, o `~/.config/opencode/commands/bflow.md` (una vez por máquina) | La sesión principal interpreta `next`; es el mismo cuerpo que la skill de Claude, con la herramienta `question` |
| [`plugin.js`](plugin.js) | `<repo>/.opencode/plugins/bflow.js` (lo escribe `bflow render`) | Guard y medición de tokens (OpenCode 1.x) |
| [`plugin2/index.js`](plugin2/index.js) + [`plugin2/package.json`](plugin2/package.json) | `<repo>/.opencode/plugins/bflow2/` (lo escribe `bflow render`) | Lo mismo para OpenCode 2.x, más la inyección del estado al primer prompt |

## Modelos

OpenCode exige `proveedor/modelo`. Los agentes de bflow traen alias (`opus`, `sonnet`, `haiku`) que se traducen con `models.opencode`. Los defaults: `opus` en spec-author y reviewer, `sonnet` en implementer, security-auditor y ux-auditor, `haiku` en scout y documenter; `agents.<nombre>.model` en `bflow.yaml` los cambia. En `bflow.yaml` (equipo) el modelo va al `model:` de los agentes versionados. En el nivel superior de la config global (solo tú) va a `agent.bflow-<agente>.model` de tu `opencode.json`, nunca al repo; los `models` de un perfil se ignoran:

```yaml
models:
  opencode:
    opus: anthropic/claude-opus-4
    sonnet: anthropic/claude-sonnet-4
    haiku: anthropic/claude-haiku-4
```

Para mapear `opus`, `sonnet` y `haiku` solo para ti, ponlo en el `models:` del nivel superior de tu config global (`config.yaml`, el mismo formato; este repo no lo versiona).

`bflow stats` y `bflow doctor` (ítem `veredicto-modelos`) comparan las últimas 3 tareas con la configuración de modelos vigente contra las 3 anteriores; si rondas, rechazos o hotfixes suben, avisan a qué modelo volver. Nunca revierten solos.

Un valor con `/` se usa tal cual. Un alias sin equivalente deja al agente sin `model:` y `bflow render` y `bflow doctor` lo avisan. Si el repo define el alias, no se escribe override. `bflow render` e `install opencode` conservan el resto de tu `opencode.json`; con solo `opencode.jsonc` o un JSON ilegible no lo editan y te dan el bloque a pegar.

Pendiente de comprobar a mano (no se pudo en la implementación): que OpenCode aplique el `agent.bflow-*.model` de `opencode.json` sobre el agente de `.opencode/agents`, y si aparecen agentes fantasma `bflow-*` en un repo sin bflow.

## Plugin: guard y tokens

OpenCode 2.x no carga `bflow.js` (pide `export default { id, setup }`) y 1.x ignora la carpeta `bflow2/`, así que `bflow render` escribe los dos y cada versión usa el suyo. El de 2.x hace lo mismo con la API de 2.x: `ctx.tool.hook("execute.before")` (herramientas `shell`, `edit`, `write`, `patch`, `read`, `subagent`; los argumentos vienen en `x.input`), eventos `session.step.ended` y `session.execution.succeeded` para los tokens, y `ctx.session.hook("prompt")` con `ctx.session.synthetic` para inyectar el estado una vez por sesión raíz. Lo que sigue describe el de 1.x.

`bflow render` también escribe `<repo>/.opencode/plugins/bflow.js` (de [`plugin.js`](plugin.js); se commitea y no se edita). Solo le pasa datos a bflow por stdin, sin dependencias de npm:

- `tool.execute.before` de bash, edit, write, multiedit y patch/apply_patch corre `bflow guard --tool opencode --reads`; el de `read` solo corre si la sesión es una subsesión `bflow-reviewer` (así bflow mide cuánto del diff abrió el reviewer); con exit 2 bloquea con el motivo. Si `bflow` no está en el PATH, tarda más de 10 s o falla, la herramienta pasa y el plugin avisa una vez por sesión.
- `tool.execute.after` (1.x) y `execute.after` (2.x) de bash corren `bflow guard --tool opencode --post`: si el comando cambió una prueba congelada, bflow la restaura y el motivo se agrega a la salida que ve el modelo. Nunca lanza: el comando ya corrió.
- `message.updated` agrega cada mensaje de asistente terminado (solo metadatos) a `.bflow/cache/opencode/<sesión>.jsonl`; `session.idle` corre `bflow hook tokens --tool opencode`, que atribuye los tokens por fase, agente y modelo.
- No hay evento de fin de subagente ni de inicio de sesión: el nudge y el resumen de inicio no existen en OpenCode; lo cubren `bflow report`, `bflow check --verify` y el bloque de AGENTS.md.

`bflow doctor` muestra el estado del plugin.

## Problemas comunes

- **El plugin no carga.** `bflow doctor` avisa (`log warn`) si OpenCode registró `failed to load plugin` para un archivo de `.opencode/plugins/` después del último `bflow render`, con el motivo. Corre `bflow render` y, si sigue, revisa el log. En 2.x, el fallo de `bflow.js` con `must export a default definition` es normal: 2.x carga `bflow2/` y `bflow.js` es para 1.x.
- **El modelo de Go pide regiones Global.** El log dice `requires Global regions` y el agente se queda quieto. `bflow doctor` lo avisa si pasó en los últimos 7 días, lista qué agentes y modelos quedan bloqueados (con su origen) y deja `modelos` en warn. Dos salidas: activa Global en Privacy de tu workspace de OpenCode, o cambia el modelo sin activarlo: `agent.<nombre>.model` en `opencode.json` (solo tú) o `models.opencode` en `bflow.yaml` y `bflow render` (equipo). Un rechazo anterior al último cambio de `opencode.json` o de `.opencode/agents/bflow-*.md` ya no cuenta.
- **Certificado autofirmado.** El log dice `self signed certificate in certificate chain`, casi siempre por la inspección HTTPS de un antivirus o proxy. Define `NODE_EXTRA_CA_CERTS=<ruta a la CA del antivirus o proxy>` antes de abrir OpenCode.
- **Cómo confirmarlo en el log.** Está en `$XDG_DATA_HOME/opencode/log/` o, si no existe esa variable, en `~/.local/share/opencode/log/` (también en Windows). 2.x escribe `opencode.log`; 1.x, un archivo por corrida (`2026-10-11T022342.log`). Busca `failed to load plugin`, `requires Global regions` o `self signed certificate`.
