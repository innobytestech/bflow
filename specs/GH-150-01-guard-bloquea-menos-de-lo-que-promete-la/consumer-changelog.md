# GH-150: el guard bloquea más comandos de Bash

## Rompe compatibilidad

Comandos de Bash que antes pasaban ahora se niegan. Si un flujo los usa, hay que cambiarlo.

- **Escrituras a `.env*` y `.bflow/` desde Bash.** Se niegan con el mismo motivo que Edit/Write (`env_file` o el motivo de `.bflow/`).
  - Antes: `echo x > .env`, `cp /dev/null .env`, `rm -rf .bflow` pasaban.
  - Ahora: se deniegan. Para cambiar `.env`, edítalo a mano fuera del agente. Para bflow, usa sus comandos.
  - Permitido: `.env.example` y los artefactos del agente en `.bflow/tasks/<ID>/` (p. ej. `cat > .bflow/tasks/<ID>/contract.md`). Copiar `.env` a otra ruta sigue permitido: solo cuenta el destino.
- **Push forzado con `+`.** `git push origin +main` y `git push origin +HEAD:rama` se niegan como `force_push`.
- **Push o reset con opciones globales de git.** `git -c k=v reset --hard`, `git -C dir reset --hard`, `git --git-dir=.git push -f` y `git --no-pager -c x=y push origin <rama protegida>` se evalúan como el subcomando real y se niegan con el mismo motivo que sin opciones.
- **Globs de `.env`.** `rm .env*` se niega como `env_file`. `rm .e*` no se niega (no se reconoce).

## Qué debe hacer el consumidor

- Si un script o agente escribe en `.env` o `.bflow/` desde Bash, moverlo a Edit/Write, a un artefacto del agente, o a una edición fuera del agente.
- Si un push forzado era intencional, hacerlo fuera de bflow.

## Fuera de alcance (sin cambio)

`eval`, `base64 | sh`, intérpretes (`python -c`, `node -e`) y rutas armadas con variables no se revisan. No los trates como protegidos.
