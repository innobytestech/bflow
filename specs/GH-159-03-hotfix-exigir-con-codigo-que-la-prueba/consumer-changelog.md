# Cambios para quien consume bflow: GH-159 (hotfix exige prueba de reproducción)

## Rompe compatibilidad

Solo en el carril `hotfix`. Un `report DONE` del implementer que antes se aceptaba ahora puede salir con código 2 y `repro_test_missing`.

- **Antes:** en hotfix, `report --agent implementer --verdict DONE` avanzaba si el check del HEAD estaba en verde.
- **Después:** además exige, en la rama de la tarea, un commit anterior al último y no de merge que toque solo archivos de prueba (`guard.test_patterns`), cuyo `bflow check` falló en el paso `check.test_step`.

Error nuevo:

```
código 2
{"code": "repro_test_missing", "reason": "..."}
```

Qué hacer: hacer commit de la prueba sin tocar código, correr `bflow check <ID>` (debe fallar en el paso `test`), y después commitear la corrección y correr `bflow check <ID>` otra vez. Luego reportar DONE.

Si la tarea no puede tener prueba (docs, configuración, carreras), reportar con escape:

```
bflow report <ID> --agent implementer --verdict DONE --no-repro "<motivo>"
```

El DONE entonces abre el gate humano `repro`, no avanza solo. Una persona aprueba (`bflow approve <ID> --gate repro`) o rechaza (`bflow reject <ID> --gate repro --note "..."`, que devuelve la tarea a `implementing`).

## Cambios nuevos

- **Flag nuevo en `report`:** `--no-repro "<motivo>"`. Vacío o solo espacios da código 2 con `no_repro_invalid`. Fuera de hotfix, de un DONE del implementer o de la fase `implementing`, también da `no_repro_invalid`.
- **Gate nuevo `repro`:** aparece en `bflow next`/panel como gate pendiente. Acciones: aprobar avanza a `implementing` y deja el motivo en `decisions.md`, en un comentario del tracker y en `bflow stats` (campo `no_repro`). Rechazar solo acepta `implementing` como destino.
- **Configuración nueva:** `check.test_step` (por defecto `test`). Debe ser un nombre de `check.steps`; si no, el DONE sale con `repro_test_missing` y el motivo indica corregirlo.
- **Archivos nuevos por tarea:** `bflow check <ID>` también guarda el resultado en `.bflow/tasks/<ID>/checks/<sha>.json`. No se guarda si el árbol tiene cambios sin commitear. Es interno; no hace falta leerlo.

## Sin cambio

Carriles `light` y `full`, `bflow check --verify`, formato de `check.md` y `check.json`, y el resto de los comandos.

## Para regenerar

Correr `bflow render` para actualizar el oficio del implementer.
