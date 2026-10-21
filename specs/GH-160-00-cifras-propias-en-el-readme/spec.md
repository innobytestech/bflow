# GH-160 · [00] Cifras propias en el README

## Brief

**Objetivo:** publicar en `docs/por-que-y-rumbo.md` una tabla con las cifras reales del equipo usando bflow rc.9 (anónima y sin maquillar) y enlazarla desde el README.

**Entra**
- Sección nueva `## Cifras del equipo` en `docs/por-que-y-rumbo.md`, después de `## Lo que medí` (que pasa a decir que esas cifras son del harness viejo).
- Tabla por repo y dev copiada del resumen anónimo (`C:\jaad\bflow-cifras\resumen-anonimo.md`, fuera del repo): 51 tareas terminadas en 6 repos.
- Lo propio y lo de otros separado: dev 1 (el autor) 48 terminadas; dev 2 y dev 3, 3. Sin sumar ambos grupos como "el equipo".
- Notas fijas: versión medida (rc.9, con las gates de contrato y pausa del carril full, antes de #153); tiempo de persona = espera en gates (incluye noches y fines de semana), no tiempo trabajado; calidad tras el merge no medida, pendiente para v0.1.1; no hay comparables sin bflow; faltan datos de 2 personas.
- README: la sección `## Cifras reales` enlaza la tabla nueva y deja de decir "un solo repo"; la fila de docs se mantiene.

**No entra**
- Código, métricas nuevas ni la pregunta "¿hubo que corregir algo tras el merge?" (v0.1.1).
- Copiar al repo las carpetas `.bflow` ni el resumen fuera del repo.
- Rehacer `docs/cifras.md` (bflow en bflow, 58 tareas): se enlaza, no se mezcla con la tabla del equipo.

**Decisiones nuevas**
- [N] Sin mediana global: el resumen trae medianas por fila y la mediana de medianas no es la mediana real. Se publican totales de tareas y conteos (rechazo, ronda) por grupo; las medianas, solo por fila. Descartado: promediar las medianas.
- [N] bflow en bflow queda fuera de esta tabla y se cita aparte con enlace a `docs/cifras.md` (corte distinto, 2026-10-16). Descartado: agregarlo como "Repo G".
- [N] La tabla nueva vive solo en `por-que-y-rumbo.md`; el README da dos frases y el enlace. Descartado: duplicar la tabla en el README.

**Riesgos**
- Fuga de identidad: el directorio fuente tiene carpetas con nombres de personas. Nada de eso pasa al repo; solo "Repo A..F", tipo y "dev N".
- Muestra chica de otros devs (3 tareas): la nota lo dice tal cual para que no se lea como tendencia.
- Si llegan los datos de las 2 personas que faltan antes de cerrar (#24), se agregan filas y se recalculan totales.

**Tamaño:** S. Solo docs, dos archivos.

## Discovery

<!-- Discovery completo; lo escribe bflow al cerrar el gate discovery. No lo edites. -->

## Requirements

<!-- Carril light: los criterios van dentro de Tasks. -->

## Design

<!-- Carril light: sin design aparte. -->

## Tasks

- [x] T1. Tabla del equipo en `docs/por-que-y-rumbo.md`.
  - CUANDO se lea `docs/por-que-y-rumbo.md`, DEBE existir `## Cifras del equipo` con una fila por repo y dev con estas columnas: Repo, Tipo, Dev, Tareas, Terminadas, Mediana agente, Mediana persona, Mediana tokens nuevos, Mediana caché, Con rechazo en gate, Con ronda de calidad; los valores, idénticos a `resumen-anonimo.md`.
  - DEBE separar dos grupos (dev 1: 48 terminadas; otros devs: 3) con sus totales de terminadas, con rechazo y con ronda, sin un total combinado ni medianas globales.
  - DEBE incluir las notas: rc.9 antes de #153; tiempo de persona = espera en gates, no tiempo trabajado; calidad tras el merge no medida (pendiente v0.1.1); sin comparables sin bflow; faltan 2 personas.
  - DEBE enlazar `cifras.md` como medición aparte de bflow en bflow.
  - NO DEBE aparecer ningún nombre de persona, nombre de repo privado ni ID de tarea de otro repo (`grep` contra los nombres de las carpetas fuente da vacío).
- [x] T2. `## Lo que medí` deja claro que sus cifras (7.000 tokens, 73-80%) son del harness anterior, y apunta a `## Cifras del equipo` para las de bflow.
- [x] T3. README: `## Cifras reales` resume en ≤3 frases las dos mediciones (equipo y bflow en bflow), enlaza `docs/por-que-y-rumbo.md#cifras-del-equipo` y `docs/cifras.md`, y deja de afirmar "un solo repo". Los enlaces resuelven.
