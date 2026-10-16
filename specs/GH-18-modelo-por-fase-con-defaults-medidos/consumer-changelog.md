# Cambios para consumidores · GH-18 (modelo por fase)

No rompe compatibilidad: los campos y líneas nuevos son aditivos. Lo único que cambia de comportamiento es el modelo por defecto de dos agentes.

## Cambia el modelo por defecto de spec-author y reviewer

- Antes: spec-author heredaba el modelo de la sesión y reviewer usaba `sonnet`.
- Ahora: ambos usan `opus` (`model: opus` en `.claude/agents/bflow-spec-author.md` y `bflow-reviewer.md`).
- Qué hacer:
  - Si quieres el modelo anterior, pon `agents.spec-author.model` o `agents.reviewer.model` en `bflow.yaml` (por ejemplo `sonnet`) y corre `bflow render`.
  - Si usas OpenCode, mapea `opus` en la config global (`models.opencode.opus`, solo para ti) o en `bflow.yaml`. Sin mapeo, esos dos agentes quedan sin `model:` y usan el de tu OpenCode.
  - Los agentes generados cambian: corre `bflow render` (o `bflow render --check` en CI, que fallará hasta regenerarlos).

## `bflow start` escribe `data.models` en el evento `start`

- Campo nuevo en el log: `data.models`, un mapa agente → modelo (incluye `scout`). Un modelo vacío significa "heredado".
- Qué hacer: nada, salvo que tu parser del log rechace claves nuevas.

## `bflow stats` (sin ID) agrega la línea `modelos:` y `data.models`

- Texto: una línea nueva `modelos: ...` después del resumen, solo cuando hay datos de modelos. Estados: `sube`, `sin_regresion`, `sin veredicto` (menos de 3 tareas por lado). Si no hay datos, la línea no aparece.
- JSON (`--json`): `data.models` con el veredicto: `verdict` (`sube` | `sin_regresion` | `sin_veredicto` | `sin_datos`), `rose` (qué subió), `changes` (cada uno con `agent`, `from`: el modelo anterior al que volver, y `to`: el vigente) y las ventanas `current` y `previous`.
- Qué hacer: si parseas `stats --json`, acepta la clave nueva. Si ves `sube`, cambia el agente de `changes` a su `from` con `bflow.yaml` y corre `bflow render`. bflow no revierte nada por su cuenta.

## `bflow stats <ID>` muestra el origen de los modelos

- Texto: línea `modelos (<origen>): agente=modelo,...`. El origen es `start` (la tarea tiene `data.models`) o `tokens` (se infirió de los eventos de tokens de una tarea anterior). Sin datos, no aparece.
- JSON: `data.stats.model_set` y `data.stats.model_source`.

## `bflow doctor` tiene el ítem `veredicto-modelos`

- Estados: `ok` (sin regresión o sin datos) o `warn` (sube, o no se pudo leer el log). Nunca `fail`: el código de salida no cambia.
- Qué hacer: si tu script lee la lista de ítems de `doctor`, acepta el ítem nuevo. Un `warn` no rompe tu pipeline.
