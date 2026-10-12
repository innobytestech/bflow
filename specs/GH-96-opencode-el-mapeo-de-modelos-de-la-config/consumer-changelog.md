# Cambio para consumidores: modelos personales de OpenCode (GH-96)

## Rompe compatibilidad

**Los agentes versionados ya no traen `model:`.** `bflow render` escribe `.opencode/agents/bflow-*.md` solo con la tabla `models` de `bflow.yaml`. Si el alias de un agente no está ahí, el archivo sale sin `model:` y OpenCode usa su modelo por defecto.

Qué hacer:

1. Corre `bflow render` en cada repo (o `bflow install opencode`).
2. Si tu mapeo personal está en la config global (`models.opencode`), `render` escribe el modelo en tu `opencode.json`, en `agent.bflow-<agente>.model`. No hace falta tocar nada más.
3. Si quieres que el modelo quede fijo para todo el equipo, ponlo en `models` de `bflow.yaml` y vuelve a correr `render`.
4. Confirma con `git status`: tras el render no debe quedar ningún cambio sin commitear en `.opencode/agents/`.

## Cambios de comportamiento

- **`opencode.json` del usuario.** `render` (sin `--check`) y `bflow install opencode` escriben solo la clave `agent.bflow-<agente>.model`, para los alias que cubre tu mapeo personal y no cubre el repo. Las demás claves se conservan, pero el archivo se reescribe en JSON con claves ordenadas (2 espacios). Si no hay cambios, no se escribe.
- **Ruta.** `$XDG_CONFIG_HOME/opencode/opencode.json` o `~/.config/opencode/opencode.json`, también en Windows.
- **`.jsonc` o JSON ilegible.** No se edita. `render` muestra el bloque `{"agent":{...}}` para pegarlo a mano.
- **Perfil con `models`.** Un perfil de la config global que defina `models` se ignora y `render` y `doctor` muestran un aviso.
- **Alias en el repo y en la config global.** Gana `bflow.yaml`; no se escribe override personal para ese alias.
- **`render --check`.** Ignora el mapeo global y solo compara con la tabla del repo.
- **Fallo al escribir `opencode.json`.** `render` termina bien y muestra un aviso; los archivos del repo ya se escribieron.

## Errores nuevos

Ninguno con código HTTP. Los avisos aparecen en la salida de `render`, `install opencode` y `doctor`; `data.personal_models` en la salida JSON de `render` describe la sincronización.

## Limitación

Si dos repos definen alias distintos para el mismo agente (`agents.<nombre>.model`), `opencode.json` es uno por usuario y gana el último `render`. `doctor` lo marca como desincronizado. Para evitarlo, fija los modelos en `bflow.yaml`.
