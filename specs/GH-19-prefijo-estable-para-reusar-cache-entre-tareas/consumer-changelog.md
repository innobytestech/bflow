# Cambios para consumidores · GH-19

`bflow stats <ID>` ahora mide el costo del prefijo por agente (instrucciones y herramientas) y lo muestra en la tabla "por agente", así como un nuevo campo JSON cuando se usa `--json`.

## Nueva columna `prefijo` en la tabla "por agente"

**Cambio.** `bflow stats <ID>` ahora muestra una columna `prefijo` en la tabla "por agente", que indica la caché escrita en la primera llamada de cada corrida del agente, promediada entre sus corridas.

**Antes (sin detalle del prefijo)**

```
$ bflow stats GH-1

...
por agente:
  sesión principal    2,120    8,900   8    9.2k    7.2k
  implementer           890    5,400   3    8.2k    6.5k
```

**Ahora (con prefijo y escrita)**

```
$ bflow stats GH-1

...
por agente:
  sesión principal    2,120    8,900   8    9.2k    7.2k   7,200    8,500
  implementer           890    5,400   3    8.2k    6.5k   5,100    8,200
                      nuevos   caché  llamadas ctx máx ctx final escrita prefijo
```

- `escrita`: caché escrita total del agente en todas sus llamadas. Es "-" si está calculando desde el log (tareas sin `calls.jsonl`).
- `prefijo`: caché escrita en la primera llamada de cada corrida, promediado entre corridas. Es "-" si no hay `calls.jsonl` o el agente no tiene corridas.

Si `prefijo` es parecido en todas las corridas de un agente, el prefijo es estable y la caché se reusa entre tareas (limitado por la duración de la caché en el modelo, unos minutos).

**Qué tiene que hacer el consumidor**

Si parseas `bflow stats <ID>` en texto:
- La tabla "por agente" tiene dos columnas nuevas al final. Ajusta tus parsers o scripts que esperen un número fijo de columnas.
- El ancho de la tabla creció de ~70 a ~88 caracteres.

Si solo consumes texto para lectura humana, no requiere cambios.

## Nuevo campo `prefix` en JSON

**Cambio.** `bflow stats <ID> --json` ahora incluye `data.stats.prefix`, un mapa con el costo del prefijo por agente.

**Antes (sin detalle del prefijo en JSON)**

```bash
$ bflow stats GH-1 --json

{
  "ok": true,
  "data": {
    "stats": {
      "agents": {
        "implementer": { "new": 890, "cache_read": 5400, "calls": 3, ... },
        "main": { "new": 2120, "cache_read": 8900, "calls": 8, ... }
      }
    }
  }
}
```

**Ahora (con campo `prefix`)**

```bash
$ bflow stats GH-1 --json

{
  "ok": true,
  "data": {
    "stats": {
      "prefix": {
        "implementer": { "runs": 3, "sum": 24600, "avg": 8200 },
        "main": { "runs": 8, "sum": 68000, "avg": 8500 }
      },
      "agents": {
        "implementer": { "new": 890, "cache_read": 5400, "calls": 3, ... },
        "main": { "new": 2120, "cache_read": 8900, "calls": 8, ... }
      }
    }
  }
}
```

Campo `prefix` (omitido si no existe `calls.jsonl` o está vacío):
- `runs`: número de corridas del agente.
- `sum`: suma total de caché escrita en la primera llamada de cada corrida.
- `avg`: promedio redondeado al entero más cercano.

**Qué tiene que hacer el consumidor**

Si parseas `bflow stats <ID> --json`:
- El campo `data.stats.prefix` es nuevo y omitido si no hay `calls.jsonl` (retrocompatible).
- Si tu código espera que `data.stats` tenga siempre las mismas claves, actualiza tus parsers para ignorar o manejar `prefix` si está presente.
- El campo `data.stats.agents.<agente>.cache_write` no cambió.

**Retrocompatibilidad**

El cambio es retrocompatible:
- Si la tarea no tiene `calls.jsonl` (tareas antiguas o re-corridas sin `--calls`), `data.stats.prefix` no aparece en el JSON.
- La presencia o ausencia del campo no afecta a `data.stats.agents` ni a ningún otro campo existente.

## Checklista

Si consumes `bflow stats`:

- [ ] Si parseas la tabla "por agente" en texto, verifica que tus expresiones regulares o parsers manejen las dos columnas nuevas (`escrita`, `prefijo`).
- [ ] Si parseas `bflow stats <ID> --json`, asegúrate de que tu código maneja el nuevo campo `data.stats.prefix` (puede estar ausente en tareas antiguas).
- [ ] Si muestras la tabla "por agente" en interfaces, ajusta el espacio horizontal para las dos columnas nuevas (~18 caracteres adicionales).
