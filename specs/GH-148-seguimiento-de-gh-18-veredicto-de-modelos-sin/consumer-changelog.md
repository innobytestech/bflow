# Changelog para consumidores · GH-148

Ningún cambio rompe compatibilidad. Hay un valor nuevo aditivo y un cambio de comportamiento en el veredicto.

## Cambio de comportamiento: veredicto de modelos

El veredicto de modelos compara el modelo que de verdad corrió (lo que respondió el proveedor en los eventos `tokens`), no solo el configurado en el start.

- Antes: `bflow stats` comparaba la configuración del start, aunque en OpenCode sin `models.opencode` el agente usara el modelo de la sesión.
- Ahora: por agente, el modelo sale de sus `tokens`; `data.models` del start queda de respaldo para los agentes sin `tokens`.
- Qué puede ver el consumidor: tareas viejas de OpenCode pueden dejar de coincidir con la config del start y el veredicto bajar a `sin_veredicto`. Usar Claude y OpenCode a la vez puede partir los grupos "antes" y "después".
- Qué tiene que hacer: nada en la integración. Si un script lee el veredicto, trata `sin_veredicto` como resultado válido.

## Nuevo valor de `model_source`

En `bflow stats <ID> --json`, el campo `model_source` puede valer ahora:

```json
{"model_source": "start+tokens"}
```

Valores: `start` (solo config del start), `tokens` (solo tokens) y `start+tokens` (mezcla: parte del conjunto viene de la config). Si tu cliente compara con una lista cerrada, agrega `start+tokens`.

## Salida de `bflow stats` sin ID

Con al menos una tarea y veredicto `sin_datos`, la salida incluye la línea:

```
modelos: sin datos de modelos todavía
```

Con historial vacío no cambia: sigue `sin historial todavía`, sin la línea de modelos.

Si tu parser busca la línea `modelos:` para decidir algo, ahora también la recibe cuando no hay datos.
