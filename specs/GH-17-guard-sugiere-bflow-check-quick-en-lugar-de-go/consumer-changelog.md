# Changelog para consumidores · GH-17

## Cambia el resultado de `bflow guard --post` tras un Bash de prueba

Sin ruptura de compatibilidad para comandos, config ni JSON existente. Hay un resultado nuevo que el cliente del hook debe tolerar.

### Qué cambia

Cuando el repo define `check.quick` y el agente corre la prueba del stack directo (`go test`, `cargo test`, `dotnet test`, `npm test`, `npm run test`, `pnpm test`, `yarn test`, `npx vitest|jest`, `pytest`, `python -m pytest`), `bflow guard --post` responde:

- Código de salida `2`. El comando de prueba ya corrió; el aviso no bloquea nada.
- Stderr: `bflow guard: \`<comando>\` sí corrió (aviso, no bloqueo). Para la próxima, \`bflow check --quick <paquete>\` corre las pruebas rápidas de este repo con menos salida.`
- Con `--json`, envelope con `ok: true`, `code: "advice"` y `data.reason` con el mismo texto (sin `next`).

Límites:

- Solo una vez por sesión de agente. La marca vive en `.bflow/cache/advice/`.
- Sin sesión, avisa en cada ejecución.
- Si `check.quick` no está configurado, no hay aviso y la respuesta es la de siempre.
- Si al mismo tiempo se restauran pruebas congeladas, gana el rechazo `frozen_test` (exit 2, código `frozen_test`), como antes.

### Qué tiene que hacer el consumidor

- Claude Code (PostToolUse): tratar exit 2 de `guard --post` con `code: "advice"` como aviso informativo, no como fallo del Bash ya ejecutado.
- OpenCode: el plugin agrega el texto a `output.output`; no hay cambio de código. Hace falta `bflow render` en cada repo para regenerar `.opencode/plugins/bflow2/`.
- Si un script propio parsea la salida de `guard --post` y trata cualquier exit distinto de 0 como error, debe reconocer `code: "advice"`.

### Sin cambios

- Comandos, flags y configuración.
- `bflow guard --pre` y las reglas que bloquean.
- Formato de `check`.
