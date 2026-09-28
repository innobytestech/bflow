---
name: bflow
description: Conduce el flujo SDD con bflow. Úsala al empezar, avanzar o retomar una tarea, cuando pregunten "qué sigue", mencionen un ID de tarea o aprueben o rechacen algo del flujo.
argument-hint: "[ID]"
---

# bflow

bflow guarda el estado y decide las transiciones; tú hablas con el humano y lanzas agentes. Así el flujo no depende de reglas que haya que recordar.

Estado: !`bflow status $ARGUMENTS --json`

Todo comando `bflow … --json` responde con `next`. Haz lo que diga y luego sigue el `next` de esa respuesta:

- `ask`
  1. Si trae `show`, corre cada comando y muestra su salida completa: es lo que el humano necesita para decidir.
  2. Si trae `skill`, aplica esa sección de abajo.
  3. Pregunta con AskUserQuestion usando `question` y los `label` de `options`.
  4. Corre el `command` de la opción elegida. Si `needs_note`, cambia `<motivo>` o `<decisión>` por las palabras del humano, entre comillas.
- `spawn`: lanza cada agente de `agents` con el subagente `subagent` (todos en el mismo mensaje si `parallel`; si no existe, corre `bflow render`). Pásale solo sus `args` y su comando `report`; no pegues specs ni código, el agente lee las rutas. Al terminar, el último `report` trae el siguiente `next`.
- `wait`: dile al humano qué se espera (`reason`).
- `done`: no hay nada pendiente.

Si un comando sale con código 2, muestra `data.reason` y pregunta cómo seguir; repetirlo igual dará el mismo rechazo.

## discovery
Pregunta hasta 3 cosas por tanda (alcance, datos clave, errores, restricciones), cada una con tu recomendación. Lo que se contesta leyendo el repo, léelo. Al cerrar, guarda el discovery completo en un archivo y corre el comando de la opción con esa ruta.

## approve
Si el brief lista decisiones `[N]`, ratifica cada una (A = lo decidido, B = la alternativa descartada) antes de preguntar si se aprueba.

## walkthrough
Primero hasta 4 preguntas de producto sin revelar qué hace el código; compara las respuestas con el review-map. Después recorre los hunks 🔴 uno por uno.
