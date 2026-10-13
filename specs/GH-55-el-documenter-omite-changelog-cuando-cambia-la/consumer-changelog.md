# Changelog para consumidores · GH-55

No rompe compatibilidad. Los cambios solo actúan si el repo declara `flow.public_paths`; sin esa clave, el comportamiento es el de antes.

## Nuevo: `flow.public_paths` en `bflow.yaml`

Lista las rutas del repo que cambian lo que ve quien usa el producto.

```yaml
flow:
  public_paths: [cmd/, internal/cli/, internal/config/config.go, "!*_test.go"]
```

- Una entrada que termina en `/` cubre la carpeta entera.
- Las demás se comparan con `path.Match` contra la ruta completa; si el patrón no tiene `/`, también contra el nombre base.
- Una entrada que empieza con `!` excluye las rutas que coincidan.
- Deben ser rutas del repo: no absolutas, sin `..` y fuera de `.bflow/`. La validación de `bflow.yaml` rechaza las que no cumplan.

Qué hacer: si tu repo quiere este gate, agrega la clave. Si no, no hagas nada.

## Nuevo rechazo: `changelog_required` en `bflow report <ID> --agent documenter --verdict DONE`

Aplica solo si el diff contra la base toca alguna ruta de `flow.public_paths` y el changelog para consumidores no cambió, ni en commits ni sin commitear.

- Salida: exit code 2, sin cambio de fase (la tarea sigue en `documenting`).
- Mensaje (en español): lista las rutas públicas tocadas y la ruta del changelog a escribir. Indica que, si no hace falta, se reporte BLOCKED con el motivo, o que una persona puede eximirlo con `bflow unblock <ID> --no-changelog --note "motivo"`.

Qué tiene que hacer el consumidor: el documenter escribe el changelog en la ruta indicada y reporta DONE de nuevo. Un agente que no quiera escribirlo reporta BLOCKED con el motivo; no puede eximirse a sí mismo.

## Nuevo: `bflow unblock <ID> --no-changelog --note "motivo"`

- Exime a documenting del changelog para esa tarea. Solo lo puede usar una persona (`unblock` ya es de uso humano).
- Exige `--note`. Sin nota sale con `note_required` (exit 2) y no cambia el estado de la tarea.
- La exención queda en la bitácora de la tarea como `data.no_changelog: true`, con el motivo.
- Vale solo para la última fase a documenting: una exención anterior a la última entrada a documenting no sirve.

## Campo nuevo en la bitácora

- Entradas `unblock`: `data.no_changelog` (booleano, opcional, solo aparece cuando es `true`).

## Sin cambio

- Comandos, flags y salidas existentes siguen igual.
- `bflow check` no avisa de changelog; el gate solo actúa al reportar DONE del documenter.
