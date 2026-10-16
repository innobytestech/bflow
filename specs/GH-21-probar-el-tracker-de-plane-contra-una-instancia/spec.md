# GH-21 · Probar el tracker de Plane contra una instancia real

## Brief

**Objetivo:** que `Create` de Plane deje la tarea en Backlog igual que GitHub, y validar el adaptador de Plane contra una instancia real.

**Entra**
- `Create` (internal/adapters/tracker/plane/plane.go) resuelve el estado de la fase backlog con `writeState` y lo manda como `state` en el mismo POST a `/work-items/`.
- Si el proyecto no tiene estado para backlog, `Create` falla antes del POST con el mismo mensaje que `Transition` (sugiere `bflow tracker setup` o ajustar `tracker.states`). No se crea el work item.
- El fake de plane_test.go deja de poner `st-backlog` a ciegas: usa el `state` del POST y, si no viene, el estado por defecto del fake, que pasa a ser `Todo`. Así una regresión ya no queda oculta.
- Prueba manual contra un proyecto real de Plane: `bflow task add`, `bflow new`, `bflow start`, transiciones de fase, bloqueo y cierre. El resultado se anota en la sección `### Prueba manual` de esta spec. Solo se registra la versión de Plane y el resultado de cada paso, nunca la URL privada, el token ni el workspace.

**No entra**
- Un harness automático de integración contra Plane real ni variables de entorno de CI para eso.
- Cambios a `EnsureStates`, a `tracker.states` ni al adaptador de GitHub.
- Corregir lo que salga en la prueba manual y no sea `Create`: cada hallazgo se abre como issue aparte.

**Decisiones nuevas**
- [N] Forzar backlog en `Create`. Descartado: dejar el estado por defecto del proyecto. Ese estado puede ser Todo, que bflow traduce a otra fase, o ningún estado del flujo, y entonces la tarea nace fuera del intake.
- [N] Mandar `state` en el POST. Descartado: hacer un PATCH después del POST, como GitHub con Projects. Serían dos llamadas y, si la segunda falla, el item queda en un estado intermedio. La API v1 de Plane acepta `state` (UUID) al crear el item.
- [N] Sin estado de backlog, `Create` falla sin crear nada. Descartado: crear en el estado por defecto y avisar. Es lo mismo que hace GitHub, que valida la opción antes de crear el issue.

**Riesgos**
- Si alguna versión de Plane ignorara `state` en el POST, la prueba manual lo detectaría (paso 1). En ese caso se reporta y se pasa al PATCH.
- Un proyecto que hoy funciona sin estado Backlog empezaría a fallar en `task add`. El mensaje indica cómo arreglarlo y `bflow tracker setup` lo crea.

**Tamaño:** S. Son unas 10 líneas en plane.go, ajustes al fake, 2 pruebas y la prueba manual.

## Discovery

<!-- Discovery completo; lo escribe bflow al cerrar el gate discovery. No lo edites. -->

## Requirements

<!-- Criterios EARS numerados R1..Rn, cada uno [D] (del discovery) o [N] (nuevo). -->

## Design

<!-- Estructuras, firmas, decisiones tomadas y descartadas, superficie de seguridad. -->

## Tasks

- [x] T1 · `Create` fuerza backlog.
  - En plane.go, `Create` llama a `c.writeState(sts, flow.Backlog)`. Si no hay estado, devuelve el error con el formato de `Transition` (fase backlog y los nombres de `c.States[flow.Backlog].Names`) sin hacer el POST.
  - El body pasa a `map[string]any{"name", "description_html", "state": st.ID}`.
  - Se actualiza el comentario de la función.
  - El fake del POST `/work-items/` usa `in["state"]` si viene y, si no, `st-todo`.
  - Criterios:
    - CUANDO se crea una tarea en un proyecto con estado de backlog, el POST DEBE incluir `state` con el id de ese estado, y la tarea devuelta DEBE tener `State` "Backlog" y `Phase` backlog. Lo cubre la prueba `TestCreateLeavesBacklog` en plane_test.go, que también afirma el `state` recibido por el fake.
    - CUANDO el proyecto no tiene estado para backlog, `Create` DEBE devolver un error que mencione `bflow tracker setup` y NO DEBE hacer el POST. Prueba: `TestCreateWithoutBacklogState` (el fake no tiene `st-backlog` y se afirma que `len(f.items)==0`).
    - `TestCreate`, `TestCreateError`, `TestConformance` y `TestPlaneWiring` (cmd/bflow/e2e_plane_test.go) DEBEN seguir pasando.
- [x] T2 · Prueba manual contra un Plane real. La hace el humano, o el implementador si tiene credenciales; si nadie las tiene, el paso queda marcado como pendiente en el PR.
  - Se usa un proyecto desechable cuyo estado por defecto no sea Backlog, se compila el binario de la rama y se corren estos pasos:
    1. `bflow task add` deja el item en Backlog.
    2. `bflow new` deja el item en Backlog.
    3. `bflow start` pasa a Discovery o Spec pendiente según el carril y pone la fecha de inicio.
    4. Transiciones hasta Por PR.
    5. Bloquear y desbloquear (Bloqueado).
    6. Cierre a Done.
    7. Proyecto sin estado Backlog: el error y luego `bflow tracker setup`.
  - El resultado va como `### Prueba manual` al final de Tasks: versión de Plane y ok/falla por paso, sin URL, token ni workspace.
  - Criterio: CUANDO un paso falle por algo que no sea `Create`, se DEBE abrir un issue y enlazarlo, no corregirlo en esta rama.
- [x] T3 · En docs/configuracion.md, sección de Plane, agregar una línea: las tareas nuevas nacen en el estado de la fase backlog y, si el proyecto no lo tiene, `bflow tracker setup` lo crea.

### Prueba manual

Pendiente: el implementador no tiene credenciales de una instancia real de Plane; la hace un humano antes de mergear (pasos 1-7 de T2).
