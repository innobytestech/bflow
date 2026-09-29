# Cambios

Los cambios de bflow que ve quien lo usa. Las versiones siguen [semver](https://semver.org/lang/es/) desde la v0.1.0; antes de la 1.0, la API de comandos puede cambiar entre versiones menores.

## v0.1.0

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
- **Panel en vivo (`bflow watch`).** Encabezado de una línea, la tarea y su carril, la línea de progreso (lo hecho en verde, la fase actual resaltada, lo que falta atenuado), AHORA y DESPUÉS en palabras llanas con quién actúa (AGENTE en cian, TÚ en amarillo cuando la tarea espera tu decisión), tiempos por fase, tokens por agente, fricción y últimos eventos. Muestra el banner completo si cabe y una línea si no. Se ajusta al alto de la terminal y se repinta sin parpadeo. `watch --open` lo abre en otra pestaña o ventana y no duplica uno abierto (en Windows Terminal, con tu perfil por defecto); con `ui.watch: true`, `start` lo abre solo, nunca en CI ni sin escritorio.
- **Contrato sin pruebas huecas.** `report CONTRACT_READY` rechaza (`contract_hollow`) las pruebas de la tarea que se saltan siempre o no tienen cuerpo (Go, JS/TS, Python, Java/Kotlin, C#), con archivo y línea: al aprobar el contrato se congelan y ya no se podrían completar.
- **Volver a contract o a spec desde una decisión.** Si el implementer descubre que el contrato o la spec no alcanzan, la gate de decisión ofrece rehacerlos; al aprobar el contrato otra vez, las pruebas se vuelven a congelar solas.
- **`bflow ui`: el panel en el navegador.** Lo mismo que `watch` (riel de progreso del carril, AHORA y DESPUÉS, tiempos, tokens por agente, fricción y eventos) en una página local de solo lectura en `127.0.0.1:7719`, con tema claro y oscuro y diseño sobrio: solo resalta lo que te toca. Rechaza cualquier otro `Host` y no carga nada de internet. Si das permiso, el navegador avisa cuando la tarea espera tu decisión. Con `ui: { web: true }` (o `watch --web`), la ventana del panel también sirve la página y abre el navegador; al cerrarla se cierra la página.
- **Nada sin commitear se queda fuera del PR.** `report DONE` del implementer y del documenter, y la aprobación que abre el PR, se rechazan (`uncommitted`) si hay cambios de código sin commitear, con la lista de archivos. La spec no cuenta: bflow la commitea sola.
- **Preguntas de producto con opciones.** El reviewer escribe 2 o 3 respuestas posibles por pregunta; la sesión las presenta con AskUserQuestion, sin decir cuál hace el código.
- **Aviso cuando la tarea te espera.** Al terminar el turno de la sesión principal, si hay una gate pendiente o la tarea está bloqueada, bflow manda una notificación del sistema (Windows, macOS o Linux con `notify-send`) que dice qué hay que decidir. Una por espera; `ui: { notify: false }` la apaga; nunca en CI ni sin escritorio.
- **Instalación sin Go y `bflow update`.** Cada tag `vX.Y.Z` publica binarios para Windows, Linux y macOS (amd64 y arm64) con `checksums.txt` y atestación de procedencia; los `.exe` se firman cuando el repo tiene el certificado. `install.sh` e `install.ps1` instalan la última versión verificando el SHA-256; `bflow update` la actualiza (`--check` solo avisa) y no instala nada que no coincida con `checksums.txt`.
- **`doctor` revisa el co-autor:** con `guard.forbid_coauthor`, avisa si Claude Code sigue agregando `Co-Authored-By` (falta `"attribution": { "commit": "" }` en su `settings.json`), porque cada commit se rechazaría y se repetiría.
- **`doctor` mide el contexto al iniciar:** estima lo que Claude Code carga en cada llamada de la sesión principal (CLAUDE.md con sus imports, reglas sin `paths:`, `MEMORY.md`, descripciones de skills y agentes) y avisa si un archivo pasa de ~4k tokens, si el total pasa de ~10k o si la memoria se corta. La estimación (~2.2 bytes por token) está calibrada con `/context` de Claude Code.
- **Tokens por agente.** `stats` desglosa los tokens por agente, con la sesión principal aparte. Cada agente cuenta en la fase donde trabajó: antes caían en la fase siguiente, porque se leían cuando el agente ya había reportado.

### Cambiado

- **Quality lleva por defecto solo al reviewer**, que revisa también seguridad, resiliencia y rendimiento y deja sus hallazgos en `reports/security.md`. El `security-auditor` pasa a ser opcional: `flow.security_audit: true` lo suma. Al actualizar, `bflow render` borra `bflow-security-auditor.md` si el repo no lo activa. Si el repo tenía ajustes en `agents.security-auditor`, la config falla con un error que dice qué hacer: pasarlos a `agents.reviewer` o poner `flow.security_audit: true`.
- `stats` y la barra de estado separan los tokens nuevos de la caché leída ("145k nuevos · 2.9M caché"). El total anterior mezclaba ambos: en la piloto, 2.9M de 3.1M eran caché, que cuesta cerca del 10% de la entrada.
- `hook tokens` en `Stop` lee solo el transcript de la sesión principal y reparte cada respuesta en la fase en que ocurrió; en `SubagentStop`, solo el del subagente (`agent_transcript_path`).
- Con UI, la gate de spec muestra también el UI blueprint.
- `ui-designer` y `ux-auditor` terminan de inmediato si la tarea no toca la interfaz.
- `init` usa `agent: claude` si el repo ya tiene `.claude/` o `CLAUDE.md`, y propone `bflow render`.
- `settings.json` del adaptador de Claude agrega el hook `SubagentStop` con `matcher: "bflow-.*"`. Los repos existentes deben agregarlo; `doctor` avisa.

## MVP (sin versión)

Motor de flujo con gates humanas y carriles, tracker local y Plane, git y GitHub, compuerta determinista (`check`), guardas por hook, métricas por fase y adaptador de Claude Code.
