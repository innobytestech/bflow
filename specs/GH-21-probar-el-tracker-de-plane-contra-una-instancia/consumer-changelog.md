# Changelog para consumidores: GH-21

## Cambio de comportamiento: tareas nuevas en Plane nacen en Backlog

Afecta a `bflow task add` y a `bflow new` cuando `tracker.adapter` es `plane`.

**Antes:** la tarea se creaba en el estado por defecto del proyecto de Plane, que puede no ser de backlog.
**Ahora:** la tarea se crea en el estado que corresponde a la fase `backlog` (según `tracker.states`).

## Error nuevo: el proyecto no tiene estado de backlog

Si el proyecto de Plane no tiene ningún estado para la fase `backlog`, la creación se rechaza antes de llamar a la API. No se crea ninguna tarea y no hay petición HTTP.

Mensaje:

```
plane: el proyecto <PROYECTO> no tiene estado para la fase backlog (<nombres configurados>); corre bflow tracker setup o ajusta tracker.states
```

**Qué tiene que hacer el consumidor:** ejecutar `bflow tracker setup` para crear el estado faltante, o ajustar `tracker.states` para que la fase `backlog` apunte a un estado que exista en el proyecto. Después, volver a correr la creación.

## Detalle de la petición a Plane

La creación de la tarea (POST a `/work-items/`) ahora incluye el campo `state` con el UUID del estado de backlog. Un cliente que consume la API de Plane directamente no necesita cambios, pero si asume que el estado inicial lo decide el proyecto, debe dejar de hacerlo.
