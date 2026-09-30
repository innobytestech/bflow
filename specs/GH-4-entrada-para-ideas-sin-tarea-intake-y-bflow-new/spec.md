# GH-4 · Entrada para ideas sin tarea (intake) y bflow new

## Brief

**Objetivo:** que una idea sin tarea entre al flujo (tarea en el tracker + carril) y que un agente barato lea el repo antes de que nadie pregunte.

**Propuesta: SPLIT.** No cabe en el carril light: toca la máquina de estados (`internal/flow/machine.go`, `next.go`), la config del flujo, el catálogo y el render de agentes, el adaptador de Plane, la CLI, el engine y la skill (unos 12 archivos de producción). Se divide en dos tareas que se entregan por separado:

**A · Intake y `bflow new` (light)**
- `status` sin tarea activa devuelve `next` = `ask`, gate `intake`, skill `intake`, con opciones «crear» (`bflow new ...`) y «usar una existente» (`bflow start <ID> --lane ...`).
- `bflow new --lane <carril> --title <título> --file <idea.md>`: `CreateTask` + `Start` en un comando; la idea va como descripción de la tarea.
- `Create` en `internal/adapters/tracker/plane` (hoy solo tiene `Get`/`List`/`Transition`...).
- Sección `## intake` (≈5 líneas) en `adapters/claude/skills/bflow/SKILL.md`: proponer título y carril con motivo, buscar títulos parecidos con `List` antes de crear.
- [N] Sin respaldo en local cuando el tracker no implementa `Creator`: tras añadirlo a Plane, los tres adaptadores del binario (local, github, plane) lo implementan, y una tarea LOCAL-n con tracker Plane no se podría leer con `Get` (sería mudar tareas, que está fuera). Se mantiene el rechazo actual de `CreateTask`. Descartado: un tracker compuesto que enrute por prefijo.

**B · Agente `bflow-scout` (full)**
- Agente de solo lectura (modelo haiku) que corre una vez al entrar a la primera fase del carril y escribe `.bflow/tasks/<id>/reports/scout.md` (≤40 líneas).
- Cambia la máquina: en discovery el gate se abre al entrar y `report` rechaza con `gate_pending`; el scout tiene que ir antes del gate y no contar para `allReported` de spec. Necesita su veredicto (`DONE`), clave propia en `flow.Config` y tools solo `read` en el render.
- Hotfix no tiene fase spec (`DefaultLanes`): ahí correría al entrar a implementing. Esto hay que decidirlo en el discovery de B.
- Los oficios de discovery, spec-author e implementer parten de `reports/scout.md`.

**Riesgos:** B cambia el contrato de `report` y la forma de `flow.agents`; por eso va en full, con T1. A no depende de B.

## Discovery

<!-- Resumen en ≤5 líneas. El discovery completo está como comentario en el tracker. -->

## Requirements

<!-- Criterios EARS numerados R1..Rn, cada uno [D] (del discovery) o [N] (nuevo). -->

## Design

<!-- Estructuras, firmas, decisiones tomadas y descartadas, superficie de seguridad. -->

## Tasks

<!-- Checklist T1..Tn; T1 es el contrato. Cada tarea cita sus R. -->
