# Comandos

`bflow help` lista todos. Cualquier comando acepta `--json` y responde con el mismo envelope (ver [`next`](conceptos.md#el-contrato-con-el-agente-next)). Códigos de salida: `0` bien, `1` error, `2` rechazo del flujo. Los corchetes son opcionales; si omites `ID`, se usa la tarea activa.

## Flujo

| Comando | Qué hace |
|---|---|
| `status [ID] [--brief]` | Estado y siguiente paso. |
| `new --lane full\|light\|hotfix --title "t" [--file idea.md] [--slug s]` | Crea una tarea y la arranca. |
| `start <ID> --lane full\|light\|hotfix [--slug s] [--fixes ID]` | Empieza una tarea en un carril; `--fixes` liga un hotfix a la feature que corrige. |
| `approve [ID] [--gate g] [--choice n] [--note t] [--file f]` | Aprueba el gate pendiente. |
| `reject [ID] --note "motivo" [--gate g] [--to fase]` | Rechaza el gate pendiente. |
| `report [ID] --agent a --verdict V [--note] [--file] [--option o]... [--stdin]` | Un agente reporta su veredicto (`--stdin`: reporte del scout). |
| `block [ID] --reason "motivo"` | Bloquea la tarea. |
| `unblock` | Desbloquea la tarea y vuelve a su fase. |
| `drop <ID> --note "motivo"` | Retira una tarea sin terminarla. |
| `freeze [ID] [--allow <archivo>]` | Vuelve a congelar las pruebas del contrato tal como están; con `--allow` deja cambiar una una vez. Solo una persona. |
| `show [ID] task\|brief\|spec\|contract\|review-map\|questions\|decisions\|discovery\|check\|scout [--section s]` | Muestra un artefacto. |
| `task add "título" [--description t]` | Crea una tarea (trackers que lo permiten, como local). |
| `task list` | Lista las tareas abiertas del tracker. |
| `sync [ID]` | Reintenta los cambios pendientes con el tracker. |
| `import --from harness [--dir ruta] [--state-only] [--dry-run]` | Importa el harness anterior una vez. |

## Agentes

| Comando | Qué hace |
|---|---|
| `render [--check]` | Genera los agentes de bflow para la herramienta del repo; con `--check` falla si no coinciden con la configuración. |
| `install claude\|opencode [--skill-only]` | Instala la skill (o el comando) y los hooks de bflow para tu herramienta. |

## Git y PR

| Comando | Qué hace |
|---|---|
| `pr [ID]` | Publica la rama y abre o actualiza el PR de una tarea en `in_review`. |
| `panel [--sla]` | Compromisos: cierra lo mergeado o terminado en el tracker, aunque esté en otra fase, sincroniza y avisa de gates vencidos. |

## Calidad

| Comando | Qué hace |
|---|---|
| `check [ID] [--quick pkg] [--verify]` | Compuerta determinista. |
| `env check [--quiet]` | Verifica API, puertos y variables del entorno local. |
| `guard` | Hook PreToolUse: lee la acción por stdin y la bloquea (exit 2) si rompe una regla. |

## Métricas

| Comando | Qué hace |
|---|---|
| `stats [ID] [--calls] [--reads]` | Tiempo por fase (agente, humano, bloqueada), iteraciones y tokens. Sin ID, una línea por tarea. |
| `statusline` | Una línea para la barra de estado (lee una caché: milisegundos). |
| `watch [--interval 2s] [--once] [--open] [--web]` | Panel en vivo de la tarea. |
| `ui [--port 7719] [--no-open]` | El panel en el navegador, con todos tus repos. |

`stats` con ID, además:

- `--calls`: tabla de llamadas al modelo por corrida (agente o sesión principal), con contexto nuevo y caché reutilizada en cada llamada. Sirve para ver dónde se repite contexto o si el prefijo varía entre corridas.
- `--reads`: qué archivos se leyeron y cuántas veces, cuántos agentes distintos los tocaron y qué porcentaje fueron relecturas. Sin `reads-all.jsonl`, avisa "sin registro de lecturas".

## Configuración

| Comando | Qué hace |
|---|---|
| `init [--yes] [--profile p] [--tracker local\|plane\|github] [--project P]` | Crea `bflow.yaml` detectando stack, remoto, rama base y pasos de check (con github, `--project owner/N`). |
| `doctor` | Valida config, herramientas, conexiones, entorno y hooks. |
| `connect plane\|github [--token t] [--url u] [--workspace w] [--from-gh]` | Guarda una credencial en el llavero tras validarla. |
| `profile add <nombre> [--tracker --url --workspace --host --base --agent]` | Crea o completa un perfil global. |
| `profile list` | Lista los perfiles globales. |
| `profile use <nombre>` | Usa un perfil en este repo. |
| `tracker setup [--dry-run]` | Crea en el tracker los estados que las fases necesitan. |
| `tracker states` | Estados del tracker y la fase con la que bflow los lee y escribe (solo lectura). |

## Hooks

Los instala `bflow install`; no se corren a mano.

| Comando | Qué hace |
|---|---|
| `hook session-start` | Entorno, compromisos y tarea activa en pocas líneas. |
| `hook tokens` | Hook Stop y SubagentStop: suma los tokens nuevos a la fase y avisa si la tarea espera a la persona. |
| `hook subagent-stop` | Si un agente de bflow terminó sin reportar, lo hace seguir (máximo 2 veces) y después bloquea la tarea. |

## Mantenimiento

| Comando | Qué hace |
|---|---|
| `update [--check] [--force]` | Actualiza bflow a la última versión publicada. |
| `version` | Versión de bflow. |
| `help` | Lista los comandos. |
