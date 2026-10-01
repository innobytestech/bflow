# bflow

bflow guarda el estado y decide las transiciones; tú hablas con el humano y lanzas agentes. Así el flujo no depende de reglas que haya que recordar.

Estado: !`bflow status $ARGUMENTS --json`

Todo comando `bflow … --json` responde con `next`. Haz lo que diga y luego sigue el `next` de esa respuesta:

- `ask`
  1. Si trae `display`, escríbelo tal cual en tu mensaje antes de preguntar: el humano no ve la salida de las herramientas, y sin eso aprueba a ciegas. No lo resumas.
  2. Si trae `skill`, aplica esa sección de abajo.
  3. Pregunta con {{ask_tool}} usando `question` y los `label` de `options`.
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
Gate `questions`: pregunta con {{ask_tool}}, una pregunta por cada una de `display` (hasta 4) con sus respuestas posibles como opciones, en el mismo orden; el humano puede escribir otra. No digas cuál hace el código: la idea es comparar lo que espera con lo que hace. Corre la opción "Ya respondí" con sus respuestas en `--note`.
Gate `walkthrough`: después de escribir `display`, señala dónde sus respuestas difieren de lo que dice el review-map. Luego recorre los 🔴 uno por uno, mostrando el hunk (`git diff <base>...HEAD -- <archivo>`), y al final pregunta.

## intake
Gate `intake` (sin tareas en curso): si el humano no trajo la idea, pídela.
Corre `bflow task list --json`; si un título se parece a la idea, ofrece usar esa tarea (`existing`) antes de crear.
Para una tarea nueva propón título y carril con su motivo.
Guarda la idea en un archivo (no en el chat) y corre el `command` de la opción elegida con los marcadores (`<carril>`, `<título>`, `<idea.md>`, `<ID>`) reemplazados.
