# Changelog para consumidores: GH-134

## Cambios de comportamiento

### `bflow report --agent implementer --verdict DONE` puede rechazarse con `no_new_commits` (nuevo error)

Tras un `REJECTED` de la compuerta de calidad, el DONE del implementer de la ronda de corrección se rechaza si HEAD no avanzó desde el rechazo.

- Antes: el DONE se aceptaba aunque no hubiera commit nuevo.
- Ahora: código de salida 2 y motivo `no_new_commits`, con el texto: "la compuerta de calidad rechazó (ronda N) y HEAD no avanzó: no hay commit con la corrección. Aplica lo que pide la nota del rechazo, haz commit y reporta DONE otra vez."
- Qué debe hacer el consumidor: el agente aplica la corrección, hace commit y vuelve a reportar `DONE`. Si el DONE se rechaza otra vez por esto, no hay commit nuevo: revisar que el commit se hizo sobre la rama.

### La nota del `REJECTED` llega al implementer

La nota de `bflow report --verdict REJECTED --note "..."` del reviewer ya no se queda solo en la bitácora: el `next` de la ronda de corrección la incluye en la nota del implementer, una línea por reviewer que rechazó (`- <agente>: <nota>`). Antes la nota era genérica y apuntaba a `reports/`.

- Qué debe hacer el consumidor: nada en integraciones. Si leía el texto de la nota genérica (`Lee sus reportes en .../reports/`), el texto cambió a "Corrige lo siguiente y haz commit antes de reportar DONE:" seguido de las notas.

## Sin cambios

- Comandos, flags, salida JSON de los demás comandos, configuración (`bflow.yaml`) y permisos: no cambian.
