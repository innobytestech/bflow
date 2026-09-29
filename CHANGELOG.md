# Cambios

Los cambios de bflow que ve quien lo usa. Las versiones siguen [semver](https://semver.org/lang/es/) desde la v0.1.0; antes de la 1.0, la API de comandos puede cambiar entre versiones menores.

## Sin publicar

### Agregado

- **Agentes generados (`bflow render`).** Escribe `.claude/agents/bflow-<agente>.md` con el contrato de bflow (argumentos, archivos, veredictos por fase, `report`) y el oficio: uno por defecto más lo que el repo agrega en `agents:` de `bflow.yaml` (`model`, `effort`, `read`, `extra`, `omit_claude_md`). `render --check` falla si no coinciden con la configuración; `render` nunca pisa un archivo que no generó. Si el repo tiene `AGENTS.md`, mantiene en él un bloque para retomar la tarea con otras herramientas.
- **`spawn` trae `subagent`** con el nombre del agente generado.
- **`bflow show <ID> task`** entrega la tarea del tracker marcada como contenido externo (`<pasted_content>`).
- **Límite de continuaciones.** El hook `bflow hook subagent-stop` hace seguir a un agente de bflow que terminó sin reportar, con el comando `report` y las tareas abiertas; después de 2 avisos bloquea la tarea para que decida una persona.
- **Pruebas congeladas verificables sin hooks.** `report DONE` y `check --verify` rechazan si cambió una prueba congelada. `bflow freeze` acepta un cambio hecho a propósito y solo lo puede correr una persona.
- **Métricas de calidad y fricción en `bflow stats`:** rechazos por gate, hotfixes ligados con `start --fixes`, pedidos rechazados por el flujo, bloqueos de `guard` y agentes que terminaron sin reportar. Los tokens se separan por modelo.
- **`guard`** bloquea, con una tarea en curso, crear ramas o PRs a mano, y siempre la edición del estado en `.bflow/`.
- **`doctor`** revisa la versión de Claude Code (2.1.271 o posterior), los agentes generados, el hook de fin de subagente y las skills que parecen de proceso (`doctor.ignore_skills`).
- **Identidad en la terminal.** `bflow` sin argumentos muestra el banner, la tarea activa y el siguiente paso; `version` e `init` también. Solo en una terminal interactiva: para un agente o un pipe la salida no cambia.
- **Guía** [Cómo trabajar con bflow](docs/guia.md).
- **Gates visibles.** `next` de cada gate trae `display`: el brief, el contrato o el review-map ya leídos, para que la sesión los escriba en el chat antes de preguntar. En la piloto el humano aprobaba sin ver nada, porque la salida de las herramientas no se muestra.
- **Gate `questions`** antes del walkthrough: el humano contesta las preguntas de producto sin ver el código (`bflow show <ID> questions` las saca del review-map sin las respuestas) y el walkthrough compara sus respuestas con lo implementado.
- **La spec entra al PR.** Al aprobarla, bflow hace commit de ella en la rama nueva, y antes de abrir el PR hace commit de sus cambios (tareas marcadas). El mensaje sigue `vcs.commit_style` (`{type}: {id} {summary}` por defecto).
- **`report DONE` exige las tareas de la spec marcadas `[x]`** (`tasks_open`).
- **Tokens por agente.** `stats` desglosa los tokens por agente, con la sesión principal aparte. Cada agente cuenta en la fase donde trabajó: antes caían en la fase siguiente, porque se leían cuando el agente ya había reportado.

### Cambiado

- `stats` y la barra de estado separan los tokens nuevos de la caché leída ("145k nuevos · 2.9M caché"). El total anterior mezclaba ambos: en la piloto, 2.9M de 3.1M eran caché, que cuesta cerca del 10% de la entrada.
- `hook tokens` en `Stop` lee solo el transcript de la sesión principal y reparte cada respuesta en la fase en que ocurrió; en `SubagentStop`, solo el del subagente (`agent_transcript_path`).
- Con UI, la gate de spec muestra también el UI blueprint.
- `ui-designer` y `ux-auditor` terminan de inmediato si la tarea no toca la interfaz.
- `init` usa `agent: claude` si el repo ya tiene `.claude/` o `CLAUDE.md`, y propone `bflow render`.
- `settings.json` del adaptador de Claude agrega el hook `SubagentStop` con `matcher: "bflow-.*"`. Los repos existentes deben agregarlo; `doctor` avisa.

## MVP (sin versión)

Motor de flujo con gates humanas y carriles, tracker local y Plane, git y GitHub, compuerta determinista (`check`), guardas por hook, métricas por fase y adaptador de Claude Code.
