# GH-22 · Cifras reales de bflow en bflow

## Brief

Objetivo: publicar, por tarea, lo que costó hacer bflow con bflow en este repo (tiempo, tokens, rechazos, rondas) y cómo reproducirlo con `bflow stats`.

**Entra**
- `docs/cifras.md` nuevo: tabla con una fila por tarea en `done` de este repo, sacada de `bflow stats --json` en una fecha y versión fijas; columnas: tiempo total, agente, persona, bloqueada, tokens nuevos, releídos de caché, llamadas, rechazos por gate y rondas. Arriba, un resumen (mediana y total) y notas de lectura.
- README: sección corta "Cifras reales" con 3-4 números del resumen, enlace a `docs/cifras.md` y el comando para medir el propio repo.
- `PRODUCT.md` (la única "página del producto" en el repo): la línea de Evidence on Hand deja de decir "no hay cifras públicas" y apunta a `docs/cifras.md`; se conserva "no inventar".
- Enlace desde `docs/guia.md#medir` y en la lista de Documentación del README.

**No entra**
- Código nuevo en bflow (ni flag `--md` ni script): todos los campos ya salen de `bflow stats --json`.
- Comparación contra trabajar sin bflow: no hay línea base medida.
- Datos del piloto en ms-sys-backend (no están en este repo).
- Cambiar la cifra de "7.000 tokens / 73-80%" del README (otra medición).

**Decisiones nuevas**
- [N] Tabla completa en `docs/cifras.md` y solo resumen en el README. Descartada: tabla de ~57 filas en el README (lo hace ilegible).
- [N] Se publican todas las tareas en `done` al corte, sin elegir; se excluyen las abiertas y la fila "sin tarea". Descartada: una selección de tareas representativas (sesga las cifras).
- [N] El resumen usa mediana, no promedio: el tiempo de persona incluye la espera de reloj en gates (GH-29 23h, GH-61 44h) y el promedio lo distorsiona. Los valores atípicos se publican igual en la tabla.
- [N] "Página del producto" = `PRODUCT.md`. Descartada: crear un sitio web (no existe en el repo).
- [N] Sin comparación con/sin bflow; el texto lo dice explícitamente en lugar de prometer ahorro.

**Riesgos**
- Los datos (`.bflow/metrics`, transcripts) no se versionan: un tercero no reproduce estas filas exactas, solo mide su propio repo. El doc lo dice.
- Error al transcribir: mitigado generando la tabla desde el JSON, no a mano.
- Tareas antiguas sin `calls.jsonl` muestran menos detalle; se marcan con "-" en lo que falte, no con 0.

**Tamaño:** S (solo documentación, 4 archivos).

## Discovery

<!-- Discovery completo; lo escribe bflow al cerrar el gate discovery. No lo edites. -->

## Requirements

<!-- Carril light: los criterios van dentro de Tasks. -->

## Design

<!-- Carril light: sin diseño aparte. -->

## Tasks

- [ ] T1 · `docs/cifras.md`. CUANDO se tome el corte, el doc DEBE indicar fecha, versión de bflow (`bflow version`) y el comando usado (`bflow stats --json`). DEBE tener una fila por cada tarea con `phase == "done"` del corte (hoy GH-5 … GH-138 y LOCAL-1), ordenadas como las da `bflow stats`, con: total (`total_ns`), agente (`agent_ns`), persona (`human_ns`), bloqueada (`blocked_ns`), tokens nuevos (input + output + cache_write, la misma definición de "nuevos" que `internal/metrics.Usage.New`), releídos de caché (`cache_read`), llamadas, rechazos por gate (`rejections_by_gate`, p. ej. "pause 1 · walkthrough 1"; "-" si no hay) y rondas (`rounds`). Excluye tareas abiertas y "sin tarea". Un campo que no exista para una tarea se escribe "-", nunca 0. Formato de cifras igual que el texto de `bflow stats` (1h07m, 1.3M, 245k).
- [ ] T2 · Resumen y notas en `docs/cifras.md`, sobre la tabla: número de tareas, mediana y total de tiempo total, de agente y de persona; mediana y total de tokens nuevos y de caché; tareas con al menos un rechazo y con al menos una ronda. Notas: qué mide cada columna (persona = tiempo de reloj con la tarea esperando en una gate, incluye noches y fines de semana), por qué mediana, que no hay comparación sin bflow, que los datos crudos no se versionan y cómo medir el propio repo (`bflow stats`, `bflow stats <ID>`, `bflow stats --json`, `--calls`), con enlace a `docs/guia.md#medir`. Cada cifra del resumen DEBE poder recalcularse desde la tabla.
- [ ] T3 · README: sección `## Cifras reales` entre "Qué garantiza" y "Rumbo", ≤8 líneas, con 3-4 cifras del resumen de T2 idénticas a las del doc, la frase de que no hay comparación con/sin bflow, el comando `bflow stats` y enlace a `docs/cifras.md`. Agregar `docs/cifras.md` a la lista de "Documentación".
- [ ] T4 · `PRODUCT.md:43` (Evidence on Hand): sustituir "No hay ... cifras públicas" por la referencia a `docs/cifras.md` (cifras de bflow en su propio repo, con fecha de corte), conservando que no hay capturas ni testimonios y "no inventar". `docs/guia.md` sección "Medir": una línea con enlace a `docs/cifras.md` como ejemplo publicado. Verificación: ningún archivo del repo sigue diciendo que no hay cifras públicas (`grep -rn "cifras públicas"`), y todos los enlaces nuevos resuelven a archivos y anclas existentes.
