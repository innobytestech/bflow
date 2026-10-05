# Reparto de tokens por marcas de sesión (GH-51)

Cada llamada al modelo se carga a la tarea que su sesión conducía en ese momento.

## Cómo funciona

1. **Marca de sesión:** Cuando corres `bflow <ID>` o inicias un subagente `bflow-*` para una tarea, bflow anota una marca (sesión → tarea, con timestamp) en `.bflow/cache/sessions.jsonl`.

2. **Reparto:** Al terminar un turno, `bflow hook tokens` lee las marcas de la sesión y reparte cada llamada a la tarea de la última marca cuya hora sea anterior o igual a la llamada.

3. **Sin tarea:** Si una sesión no condujo ninguna tarea (por ejemplo, solo contestaste preguntas en el chat sin un comando bflow), sus tokens van a una bolsa "sin tarea".

## Vistas

- **`bflow stats` (sin ID):** Muestra un resumen global con una línea `sin tarea` si hay llamadas sin asignar.
- **`bflow watch`:** Dibuja una línea `sin tarea` en el panel en vivo si hay llamadas sin asignar.

## Detección de problemas

Si muchas llamadas caen en `sin tarea`:
- Verifica que la sesión del agente corrió `bflow <ID>` al empezar la tarea.
- Revisa la configuración del guard: debe permitir comandos bflow.
- Verifica que el prompt del subagente contenga el ID de la tarea en formato JSON (`"id":"<ID>"`).
