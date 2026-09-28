# Cómo trabajar con bflow

Esta guía es para quien ya instaló bflow y configuró un repo (ver el [README](../README.md)). Explica qué haces tú y qué hace bflow en cada paso, cómo ajustar a los agentes y qué hacer cuando algo se atora.

## Quién hace qué

| bflow (determinista) | El agente (criterio) | Tú (decisiones) |
|---|---|---|
| Estado de la tarea, fases y transiciones | Escribe la spec y el contrato | Eliges el carril |
| Crea la rama, abre el PR, cierra la tarea al mergear | Implementa, prueba y commitea | Apruebas o pides cambios en cada gate |
| Mueve la tarea en el tracker y comenta los rechazos | Revisa calidad, seguridad y UX | Resuelves las decisiones que el agente no puede tomar |
| Corre el check y bloquea lo peligroso | Documenta y prepara el walkthrough | Recorres el diff antes del PR |

En Claude Code le dices a la sesión algo como "sigamos con API-12" o "qué sigue". La skill `bflow` corre `bflow status`, lee `next` y hace lo que indica: te pregunta, lanza agentes o espera. No tienes que recordar comandos; cada respuesta de bflow dice el siguiente paso.

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
| Walkthrough | Todo revisado y documentado | El review-map: 🔴 decisiones, 🟡 lógica, 🟢 mecánico | Aprobar y abrir el PR, volver a implementar o volver a la spec |

En el walkthrough, la sesión primero te hace hasta 4 preguntas de producto sin decirte qué hace el código, y compara tus respuestas con lo implementado. Después recorre contigo los cambios 🔴 uno por uno. Es la forma de revisar sin leer todo el diff.

Nadie marca una tarea como terminada a mano: bflow la cierra cuando detecta el merge (`bflow panel`).

## Ajustar a los agentes

`bflow render` genera los agentes en `.claude/agents/bflow-<agente>.md`. Commitéalos para que todo el equipo use los mismos, y no los edites: cada archivo tiene dos partes.

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
    quality: [reviewer, security-auditor, perf-auditor]
agents:
  perf-auditor:
    extra: docs/bflow/perf-auditor.md
```

bflow le antepone el contrato: en quality escribe `reports/perf-auditor.md` y reporta APPROVED o REJECTED.

## Skills: cuáles convienen y cuáles chocan

bflow no depende de lo que tengas instalado: el estado, las gates y las reglas viven en el CLI y ninguna skill las salta. Pero una skill puede confundir al agente si le pide hacer a mano lo que ya hace bflow.

- **Conviene:** skills de oficio, como convenciones del stack, patrones de pruebas, arquitectura o design system. Describe su uso por el dominio ("al escribir handlers HTTP en Go"), no por el proceso.
- **Choca:** skills de proceso, como planear features, crear ramas, abrir PRs, mover tickets o aprobar specs. Quítalas de los repos que usan bflow.

`bflow doctor` avisa de las skills que parecen de proceso. Es una heurística; si una no choca, agrégala a `doctor.ignore_skills` en `bflow.yaml`.

Tus reglas de arquitectura en CLAUDE.md o en los documentos de `read` son bienvenidas: son el oficio. Si una contradice el flujo (por ejemplo, "haz push directo a main"), `guard` la bloquea y el agente recibe el motivo.

## Gastar menos contexto

- **La conversación se puede descartar; el hilo vive en bflow.** El estado, las decisiones y los reportes están en `.bflow/`, la spec y el tracker. Después de aprobar una gate puedes hacer `/clear`: al reiniciar, bflow le vuelve a dar a la sesión la tarea y el siguiente paso.
- **No pegues specs ni código en el chat.** Los agentes leen por ruta y por sección (`bflow show <ID> spec --section design`).
- **Agentes con modelo y esfuerzo a su medida:** los mecánicos (documenter) con un modelo menor y esfuerzo bajo.
- `bflow stats <ID>` muestra los tokens por fase y por modelo. Úsalo para ver si un cambio de configuración ahorró de verdad.

## Cuando algo se atora

| Situación | Qué pasa | Qué haces |
|---|---|---|
| Un comando sale con código 2 | El flujo no permite eso ahora (por ejemplo, aprobar algo que no está pendiente) | Lee el motivo; repetir el mismo comando dará el mismo rechazo |
| `guard` bloqueó una acción | El agente intentó algo peligroso o que le toca a bflow | Nada: el agente recibe el motivo y qué hacer en su lugar |
| DONE rechazado con `check_required` | No hay un check verde del commit actual | El agente corre `bflow check` y commitea |
| DONE rechazado con `frozen_changed` | Cambió una prueba congelada al aprobar el contrato | Si fue un error, se deja como estaba. Si la cambiaste tú a propósito, corre `bflow freeze` desde tu terminal (un agente no puede) |
| La tarea quedó bloqueada: "terminó 3 veces sin reportar" | Un agente cortó su trabajo varias veces sin reportar a bflow | Revisa qué hizo; `bflow unblock <ID>` lo relanza |
| Cambias de modelo o de herramienta a mitad de la feature | El estado y las gates siguen en bflow | En otra herramienta, pídele que corra `bflow status --json` y siga `next`. Sin hooks pierdes `guard` y el conteo de tokens, pero no el check ni las pruebas congeladas |
| El tracker no respondió | El cambio queda pendiente y se reintenta en orden | `bflow sync` o el siguiente comando lo reintenta |

## Medir

`bflow stats <ID>` separa el tiempo de cada fase en trabajo del agente, espera tuya y bloqueo, y cuenta:

- **Calidad:** rechazos por gate, rondas de calidad y hotfixes ligados (`bflow start <ID> --lane hotfix --fixes <feature>`).
- **Fricción:** pedidos que el flujo rechazó, bloqueos de `guard` y agentes que terminaron sin reportar. Si sube en un repo, algo del entorno está confundiendo a los agentes: revisa sus skills y reglas.
- **Costo:** tokens por fase y por modelo.

Antes de quitar un paso del flujo para ahorrar tokens, compara varias features: si las métricas de calidad no empeoran, ese paso sobraba.
