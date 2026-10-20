# Cambios para consumidores: GH-170

Ámbito: resultados de `bflow eval` (reviewer). No cambia comandos, flags, configuración ni el formato JSON.

## Cambia el valor de algunos campos del resultado (no es ruptura de forma)

- **Modelo reportado en empate.** Si dos modelos empatan en número de casos, el campo `model` del resultado es ahora el del caso reportado más tarde. Antes era el primero en orden alfabético. Qué hacer: si comparas `model` entre corridas, una corrida anterior puede mostrar otro nombre sin que cambie el reviewer.
- **Hallazgos contados.** Las referencias `archivo:N` a rutas sin extensión cuentan como hallazgo cuando la ruta tiene separador de directorio (`cmd/tool:4`) o su nombre base es uno de: Makefile, GNUmakefile, Dockerfile, Containerfile, Jenkinsfile, Gemfile, Rakefile, Procfile, Vagrantfile (`Makefile:3`, `Dockerfile:12`). Antes no contaban. Qué hacer: las calificaciones (`caught`, `findings`) de corridas nuevas pueden subir o bajar frente a corridas anteriores; no compares cifras entre ambas sin volver a correr.

## Corrida interrumpida

- Si al cerrar una corrida no se puede borrar su carpeta completa, la carpeta ya no aparece como corrida abierta. Antes podía reaparecer como abierta. Qué hacer: nada; si quedó una carpeta en la caché, `bflow eval --abort` la limpia.
