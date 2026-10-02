# Cómo trabajar con bflow

Esta guía es para quien ya instaló bflow y configuró un repo (ver el [README](../README.md)). Explica qué haces tú y qué hace bflow en cada paso, cómo ajustar a los agentes y qué hacer cuando algo se atora.

## Quién hace qué

| bflow (determinista) | El agente (criterio) | Tú (decisiones) |
|---|---|---|
| Estado de la tarea, fases y transiciones | Escribe la spec y el contrato | Eliges el carril |
| Crea la rama, abre el PR, cierra la tarea al mergear | Implementa, prueba y commitea | Apruebas o pides cambios en cada gate |
| Mueve la tarea en el tracker y comenta los rechazos | Revisa calidad, seguridad y UX | Resuelves las decisiones que el agente no puede tomar |
| Corre el check y bloquea lo peligroso | Documenta y prepara el walkthrough | Recorres el diff antes del PR |

Si aún no lo hiciste, corre `bflow install claude` dentro del repo: deja la skill y los hooks. En Claude Code le dices a la sesión algo como "sigamos con API-12" o "qué sigue". La skill `bflow` corre `bflow status`, lee `next` y hace lo que indica: te pregunta, lanza agentes o espera. No tienes que recordar comandos; cada respuesta de bflow dice el siguiente paso.

## Las gates: qué ves y qué decides

| Gate | Cuándo | Qué te muestra | Qué decides |
|---|---|---|---|
| Carril | Al empezar | Las opciones full, light y hotfix | Qué tan grande es el cambio |
| Discovery (full) | Antes de la spec | La sesión te pregunta alcance, datos, errores y restricciones, hasta 3 cosas por tanda | Cuándo ya no queda ambigüedad |
| Spec | Spec lista | El brief (≤35 líneas) y, si el repo tiene UI, el UI blueprint | Aprobar o pedir cambios puntuales. Las decisiones nuevas `[N]` se ratifican una por una |
| Contrato T1 (full) | Pruebas y firmas escritas | `contract.md` | Aprobar, ajustar o volver a la spec. Al aprobar, esas pruebas quedan congeladas |
| Decisión | Un agente no puede decidir solo | El problema y 2-3 opciones | Una opción o tu propia decisión |
| Pausa (full) | Implementación con el check en verde | — | Lanzar la revisión de calidad o volver a implementar |
| Rondas | Calidad rechazó dos veces seguidas | — | Dividir la feature, volver a la spec o una ronda más |
| Preguntas de producto | Todo revisado y documentado | Hasta 4 preguntas sobre el comportamiento, sin decirte qué hace el código | Las contestas con tus palabras (o las saltas) |
| Walkthrough | Después de tus respuestas | Tus respuestas junto al review-map: 🔴 decisiones, 🟡 lógica, 🟢 mecánico | Aprobar y abrir el PR, volver a implementar o volver a la spec |

Lo que necesitas para decidir llega escrito en el chat: bflow le entrega a la sesión el texto (el brief, el contrato, el review-map) y la sesión lo copia antes de preguntar. Si alguna vez te preguntan si apruebas algo que no ves, pídelo.

El walkthrough va en dos pasos que controla bflow. Primero contestas qué esperas que haga el cambio, sin ver el código. Después la sesión compara tus respuestas con lo que el código hace, señala las diferencias y recorre contigo los cambios 🔴 uno por uno. Es la forma de revisar sin leer todo el diff.

Antes de tus respuestas, el walkthrough te dice cuánto del diff abrió el reviewer: "el reviewer leyó N de M archivos del diff", los archivos que no abrió y que no son pruebas ni docs (hasta 20), y las rutas 🔴 del review-map que no están en el diff. bflow lo mide con el hook de `Read` y de Bash del reviewer; con APPROVED rechaza si algún archivo de una viñeta 🔴 del diff quedó sin abrir, si una viñeta 🔴 no empieza con la ruta en backticks o si el review-map no trae `## Docs`. Si el hook no ve los `Read` (`bflow doctor` lo avisa), dice "cobertura del reviewer: no medida" y no se exige. El documenter, por su lado, no cierra mientras una ruta de `## Docs` no cambie en la rama ni conste en `reports/docs.md` como "sin cambio" con su motivo.

Nadie marca una tarea como terminada a mano: bflow la cierra cuando detecta el merge (`bflow panel`). Si la terminaste en otra máquina o sesión, `bflow panel` también cierra la copia local cuando el tracker ya la da por hecha o cerrada, o cuando el PR de su rama está mergeado, en cualquier fase en que haya quedado, y lo dice ("terminada fuera de esta copia").

## Ajustar a los agentes

`bflow render` genera los agentes en `.claude/agents/bflow-<agente>.md` y, si `agent:` es una lista con `opencode` (`agent: [claude, opencode]`), también en `.opencode/agents/bflow-<agente>.md`. Para OpenCode, `bflow install opencode` instala el comando `/bflow` y `models.opencode` traduce los alias (`sonnet`, `haiku`) a `proveedor/modelo`; `render` también escribe `.opencode/plugins/bflow.js`, el plugin que le da a OpenCode el guard y el conteo de tokens (si `bflow` no está en el PATH, el guard no actúa y el plugin avisa una vez por sesión). OpenCode no tiene nudge de subagente ni hook de inicio: lo cubren `bflow report`, `bflow check --verify` y el bloque de AGENTS.md. Commitéalos para que todo el equipo use los mismos, y no los edites: cada archivo tiene dos partes.

- **Contrato con bflow:** qué recibe el agente, qué archivos escribe, qué veredictos puede reportar y cómo. Sale del flujo y no se configura.
- **Oficio:** cómo hace su trabajo. Trae uno por defecto y tú lo complementas en `bflow.yaml`:

```yaml
agents:
  implementer:
    model: sonnet
    effort: medium
    read: [docs/architecture/, docs/testing.md]   # lo lee antes de empezar
    extra: docs/bflow/implementer.md              # instrucciones propias
  reviewer:
    read: [docs/architecture/]
  documenter: { model: haiku, effort: low }
```

Después de cambiar `bflow.yaml` o un archivo `extra`, corre `bflow render` otra vez. En CI, `bflow render --check` falla si alguien olvidó hacerlo.

Qué poner en `extra`: lo que es propio de tu repo y no está en otro documento. Por ejemplo, el estilo de commits, cómo se nombran las pruebas, las herramientas del stack que el agente debe usar o los patrones que el equipo evita. Mejor frases concretas que énfasis: "los handlers devuelven errores con `apperr.Wrap`" sirve más que "SIEMPRE maneja bien los errores".

Con `read`, el agente no carga CLAUDE.md: las reglas le llegan por esas rutas y su contexto arranca más chico. Si tus reglas solo están en CLAUDE.md, no uses `read` o pon `omit_claude_md: false`.

Para agregar un agente propio (por ejemplo, un auditor de rendimiento), súmalo a una fase y dale su oficio:

```yaml
flow:
  agents:
    quality: [reviewer, perf-auditor]
agents:
  perf-auditor:
    extra: docs/bflow/perf-auditor.md
```

bflow le antepone el contrato: en quality escribe `reports/perf-auditor.md` y reporta APPROVED o REJECTED.

En quality trabaja por defecto solo el reviewer, que revisa también seguridad, resiliencia y rendimiento. Si quieres una segunda mirada de seguridad con su propio agente, pon `flow: { security_audit: true }`: suma el `security-auditor`, que corre en paralelo con el reviewer y cuesta sus propios tokens.

Si la tarea cambia lo que consumen otros equipos, el documenter escribe un solo changelog para ellos, versionado y en el PR. Por defecto va junto a la spec; si tu repo ya tiene un lugar para eso, dilo con `flow: { consumer_changelog: "docs/consumers/{id}-{slug}.md" }`.

## Skills: cuáles convienen y cuáles chocan

bflow no depende de lo que tengas instalado: el estado, las gates y las reglas viven en el CLI y ninguna skill las salta. Pero una skill puede confundir al agente si le pide hacer a mano lo que ya hace bflow.

- **Conviene:** skills de oficio, como convenciones del stack, patrones de pruebas, arquitectura o design system. Describe su uso por el dominio ("al escribir handlers HTTP en Go"), no por el proceso.
- **Choca:** skills de proceso, como planear features, crear ramas, abrir PRs, mover tickets o aprobar specs. Quítalas de los repos que usan bflow.

`bflow doctor` avisa de las skills que parecen de proceso. Es una heurística; si una no choca, agrégala a `doctor.ignore_skills` en `bflow.yaml`.

Tus reglas de arquitectura en CLAUDE.md o en los documentos de `read` son bienvenidas: son el oficio. Si una contradice el flujo (por ejemplo, "haz push directo a main"), `guard` la bloquea y el agente recibe el motivo.

## Gastar menos contexto

- **Lo que se carga al iniciar se paga en cada llamada.** CLAUDE.md (con sus `@imports`), las reglas sin `paths:` y el índice de la memoria automática (`MEMORY.md`) viajan en cada llamada de la sesión principal. `bflow doctor` estima cuánto pesan y avisa si un archivo pasa de ~4k tokens o el total de ~10k. Poda lo que ya no aplica y deja en el índice solo enlaces de una línea. La memoria se corta a 200 líneas o 25 KB, así que lo que pase de ahí no se carga.
- **La conversación se puede descartar; el hilo vive en bflow.** El estado, las decisiones y los reportes están en `.bflow/`, la spec y el tracker. Después de aprobar una gate puedes hacer `/clear`: al reiniciar, bflow le vuelve a dar a la sesión la tarea y el siguiente paso.
- **No pegues specs ni código en el chat.** Los agentes leen por ruta y por sección (`bflow show <ID> spec --section design`).
- **Agentes con modelo y esfuerzo a su medida:** los mecánicos (documenter) con un modelo menor y esfuerzo bajo.
- `bflow stats <ID>` muestra los tokens por fase, por agente (la sesión principal aparte) y por modelo, con lo nuevo separado de la caché leída, que cuesta cerca del 10% de la entrada. Úsalo para ver si un cambio de configuración ahorró de verdad. `bflow stats <ID> --calls` agrega una tabla por llamada del modelo, una por corrida (agente o sesión principal); el encabezado de cada una es "agente · fase" (por ejemplo `implementer · contract`, con `#2` si se repite), con el contexto de cada llamada y los acumulados. Cómo leerla: lo releído en la llamada N es el contexto de la llamada N-1, porque cada llamada vuelve a leer lo anterior de la caché; por eso el acumulado de releído crece mucho más rápido que lo nuevo. El panel web muestra una tabla en Tokens, una fila por agente con sus corridas debajo; el panel abre cada corrida en una ventana con la tabla por llamada. Las tareas registradas antes del detalle por llamada avisan que no lo tienen. La tabla "por agente" de `bflow stats <ID>` suma dos columnas: `escrita` (caché escrita total del agente) y `prefijo` (lo que escribió en la primera llamada de cada corrida, en promedio; "-" si no hay detalle por llamada). Si `prefijo` es parecido en todas las corridas de un agente, el prefijo (instrucciones y herramientas) es estable y la caché se reusa; si cambia mucho, algo del inicio varía entre tareas. La caché dura unos minutos: una corrida que arranca pasado ese tiempo la vuelve a escribir.

## Cuando algo se atora

| Situación | Qué pasa | Qué haces |
|---|---|---|
| Un comando sale con código 2 | El flujo no permite eso ahora (por ejemplo, aprobar algo que no está pendiente) | Lee el motivo; repetir el mismo comando dará el mismo rechazo |
| `guard` bloqueó una acción | El agente intentó algo peligroso o que le toca a bflow | Nada: el agente recibe el motivo y qué hacer en su lugar. Un subagente que intenta `approve`, `reject`, `unblock`, `start` o `new` recibe que la decisión es de una persona y debe terminar con `bflow report` |
| DONE rechazado con `check_required` | No hay un check verde del commit actual | El agente corre `bflow check` y commitea |
| DONE rechazado con `frozen_changed` | Cambió una prueba congelada al aprobar el contrato | Si fue un error, se deja como estaba. Si la cambiaste tú a propósito, corre `bflow freeze` desde tu terminal (un agente no puede) |
| El implementer pide cambiar una prueba congelada (NEEDS_DECISION) | La prueba del contrato está mal o quedó vieja | Si estás de acuerdo, corre `bflow freeze --allow <archivo>` desde tu terminal: el implementer la puede cambiar una vez y se vuelve a congelar cuando reporta DONE |
| El check marca `vulns (preexistente)` | Vulnerabilidades en dependencias que la tarea no tocó | Decides tú: aceptarlas en el `accept_file` del paso con fecha de revisión y abrir otra tarea, o que el implementer actualice la dependencia en esta. El `accept_file` solo lo edita una persona |
| La tarea quedó bloqueada: "terminó 3 veces sin reportar" | Un agente cortó su trabajo varias veces sin reportar a bflow | Revisa qué hizo; `bflow unblock <ID>` lo relanza |
| Cambias de modelo o de herramienta a mitad de la feature | El estado y las gates siguen en bflow | En otra herramienta, pídele que corra `bflow status --json` y siga `next`. Sin hooks pierdes `guard` y el conteo de tokens, pero no el check ni las pruebas congeladas |
| El tracker no respondió | El cambio queda pendiente y se reintenta en orden | `bflow sync` o el siguiente comando lo reintenta |

## Medir

Mientras trabajas, `bflow watch --open` abre en otra ventana un panel en vivo: el paso actual (quién trabaja o qué gate te espera), lo que sigue, tiempos, tokens por agente y los últimos eventos. Para que `bflow start` lo abra solo, pon `ui: { watch: true }` en tu config global (`%AppData%\bflow\config.yaml` en Windows, `~/.config/bflow/config.yaml` en Linux y macOS) o en `bflow.yaml`. El primer `bflow init` en tu máquina te lo pregunta y lo guarda en la config global.

Si prefieres el navegador, `bflow ui` abre lo mismo en una página local. En ella está la línea del carril: cada fase es una estación y el tren está en la actual. La página cabe en una sola pantalla (desde 1180x720) y muestra en la franja de arriba, como chips, todos los repos donde usaste bflow (los que te esperan, primero), así que si trabajas en varios ves en cuál te toca. Con `ui: { web: true }` en la config global, la ventana del panel la abre sola al empezar una tarea, y solo una vez aunque tengas varios paneles abiertos.

`bflow stats <ID>` separa el tiempo de cada fase en trabajo del agente, espera tuya y bloqueo, y cuenta:

- **Calidad:** rechazos por gate, rondas de calidad y hotfixes ligados (`bflow start <ID> --lane hotfix --fixes <feature>`).
- **Fricción:** pedidos que el flujo rechazó, bloqueos de `guard` y agentes que terminaron sin reportar. Si sube en un repo, algo del entorno está confundiendo a los agentes: revisa sus skills y reglas.
- **Revisión:** una línea `revisión:` con la última cobertura del reviewer ("el reviewer leyó N de M archivos del diff", o "no medida"). En `--json` está en `data.stats.review`. Si la tarea no tiene cobertura, la salida no cambia.
- **Costo:** tokens nuevos y de caché por fase, por agente y por modelo.

Antes de quitar un paso del flujo para ahorrar tokens, compara varias features: si las métricas de calidad no empeoran, ese paso sobraba.
