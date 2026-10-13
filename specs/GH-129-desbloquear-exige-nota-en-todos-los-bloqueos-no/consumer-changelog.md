# Cambios para consumidores · GH-129

`bflow next` ya no pide nota al desbloquear una tarea que se bloqueó de forma normal. Solo pide nota cuando el bloqueo viene de una decisión manual (`bflow approve --gate decision --manual`).

## `next` en tareas bloqueadas: la opción unblock de un bloqueo normal no pide nota

**Cambio de comportamiento.** Cuando la tarea está en `blocked` y el bloqueo no es de decisión manual (por ejemplo, `bflow block` o un bloqueo que puso el flujo), la opción `unblock` de `next` deja de pedir nota: `needs_note` ya no aparece y el comando es `bflow unblock <ID>`. Antes exigía `--note "<resultado>"`.

Los bloqueos de decisión manual no cambian: la opción sigue con `needs_note: true` y el comando `bflow unblock <ID> --note "<resultado>"`.

**Antes**

```json
{
  "action": "ask",
  "gate": "blocked",
  "options": [
    {
      "id": "unblock",
      "label": "Desbloquear y continuar (con --note el agente ve el resultado)",
      "needs_note": true,
      "command": "bflow unblock GH-13 --note \"<resultado>\""
    },
    { "id": "keep", "label": "Sigue bloqueada" }
  ]
}
```

**Ahora (bloqueo normal)**

```json
{
  "action": "ask",
  "gate": "blocked",
  "options": [
    {
      "id": "unblock",
      "label": "Desbloquear y continuar",
      "command": "bflow unblock GH-13"
    },
    { "id": "keep", "label": "Sigue bloqueada" }
  ]
}
```

**Ahora (bloqueo de decisión manual, sin cambio)**

```json
{
  "options": [
    {
      "id": "unblock",
      "label": "Desbloquear y continuar (con --note el agente ve el resultado)",
      "needs_note": true,
      "command": "bflow unblock GH-13 --note \"<resultado>\""
    },
    { "id": "keep", "label": "Sigue bloqueada" }
  ]
}
```

**Qué tiene que hacer el consumidor.** Si tu cliente lee `needs_note` para decidir si pide un texto antes de ejecutar `command`, no esperes el campo en bloqueos normales: ejecuta el comando tal cual. Si tu cliente siempre pide nota, ya no hace falta para estos casos; si la quieres, agrégala a mano con `--note`.

## Estado de la tarea: campo `block.manual`

**Cambio aditivo.** El estado local de la tarea guarda `block.manual` (`true` solo cuando el bloqueo viene de una decisión manual). El campo se omite cuando es falso, así que no hay que cambiar nada para leer estados existentes. Los estados bloqueados antes de este cambio no traen el campo: `next` los trata como bloqueo normal, por lo que su `unblock` ya no pide nota. Si necesitas la nota en uno de esos, pásala a mano con `bflow unblock <ID> --note "<resultado>"`.

## Sin cambios

- `bflow unblock` no cambió: `--note` sigue siendo opcional y `--no-changelog` sigue exigiéndola.
- Ningún código de error nuevo ni cambio en `bflow approve`.
