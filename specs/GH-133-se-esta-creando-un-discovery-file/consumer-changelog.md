# Cambios para quien consume bflow: GH-133

## Ruptura de compatibilidad

- **`.bflow/tasks/<ID>/discovery.md` ya no se crea.** Al cerrar el gate discovery, el discovery completo se escribe en la sección `## Discovery` de `specs/<ID>-<slug>/spec.md`. Si tu herramienta lee ese archivo suelto, debe leer la sección con `bflow show <ID> discovery` (o la sección `## Discovery` de la spec).
  - Tareas creadas antes del cambio conservan su `discovery.md`. `bflow show <ID> discovery` lo sigue leyendo cuando la sección de la spec está vacía.
- **El `next` del gate discovery cambió de opción.** Antes sugería `approve <ID> --gate discovery --file <discovery.md>`. Ahora sugiere `approve <ID> --gate discovery --stdin`.
  - Si tu integración lee `options[].command` del `next` y lo ejecuta tal cual, sigue funcionando. Si espera un argumento `--file`, adáptala a `--stdin`.

## Cambios por comando

### `approve --gate discovery --stdin` (nuevo)

Recibe el discovery completo por stdin. No hace falta un archivo.

Antes:

```sh
bflow approve GH-133 --gate discovery --file .bflow/tasks/GH-133/discovery.md
```

Después:

```sh
cat discovery.md | bflow approve GH-133 --gate discovery --stdin
```

- `--file` sigue funcionando sin cambios.
- Reglas:
  - `--stdin` y `--file` juntos: rechazo `usage`.
  - `--stdin` con un gate distinto de `discovery`: rechazo `bad_attachment` (`--stdin solo aplica al gate discovery`).
  - Más de 64 KiB (65 536 bytes): rechazo `discovery_too_long`. El discovery no se guarda; recórtalo y ciérralo otra vez.
- Si la escritura en la spec falla, el gate avanza igual y la respuesta trae un aviso en `warnings`: `no se pudo escribir el discovery en la spec: <motivo>`. Tu integración debe leer `warnings` y no tratar la respuesta como fallo.

### `show <ID> discovery`

- Devuelve el cuerpo de `## Discovery` de la spec.
- Si esa sección está vacía, devuelve el `discovery.md` de la tarea (tareas anteriores al cambio).
- Si no hay ninguno, error: `<ID> todavía no tiene discovery`.

### Limitación conocida

- El discovery se corta en el primer encabezado `## ` que contenga. Si el texto del discovery trae encabezados de nivel 2, `show <ID> discovery` devuelve solo lo que hay antes de ese encabezado. Usa `###` o inferiores dentro del discovery.

## Qué tiene que hacer el consumidor

1. Si lees `.bflow/tasks/<ID>/discovery.md` directamente, cambia a `bflow show <ID> discovery`.
2. Si construyes o ejecutas el comando de cierre del gate discovery, usa `--stdin` y envía el texto por stdin. Trata el límite de 64 KiB y el rechazo `discovery_too_long`.
3. Revisa `warnings` en la respuesta de `approve`.
