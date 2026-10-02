# Cambios para consumidores · GH-50

`bflow report --agent reviewer --verdict APPROVED` ahora rechaza la aprobación si el reviewer no abrió todo el código y las pruebas del diff, no solo los archivos que él mismo marcó con 🔴 en el review-map.

## APPROVED exige abrir todo el código y las pruebas del diff

**Cambio.** Antes, la cobertura se medía solo sobre los archivos 🔴 del review-map, así que el propio reviewer fijaba la vara y podía saltarse las pruebas. Ahora `APPROVED` se rechaza mientras quede sin abrir cualquier archivo de código o de pruebas del diff.

Quedan exentos:
- La documentación (lo que `review.IsDoc` reconoce).
- Los archivos bajo `specs/`.
- Los lockfiles conocidos: `go.sum`, `go.work.sum`, `package-lock.json`, `npm-shrinkwrap.json`, `pnpm-lock.yaml`, `bun.lockb` y cualquier `*.lock`.
- Los binarios que git detecta (`git diff --numstat`).

Un archivo borrado o renombrado cuenta como leído si se abrió con pathspec (`git diff <base>...HEAD -- <ruta>` o `git show` con `--`), aunque ya no exista en disco. Si el diff tiene menos de 1,500 líneas, un `git diff <base>...HEAD` completo cubre todo.

**Antes (APPROVED con solo los 🔴 leídos)**

```
$ bflow report GH-1 --agent reviewer --verdict APPROVED
```

Se aceptaba aunque hubiera archivos de pruebas sin abrir.

**Ahora (quedan archivos sin abrir)**

```
$ bflow report GH-1 --agent reviewer --verdict APPROVED --json

{
  "ok": false,
  "code": "review_incomplete",
  "data": {
    "reason": "la revisión está incompleta:\nArchivos del diff (código y pruebas) que no abriste:\n- internal/c.go\n- internal/c_test.go\nÁbrelos con Read o `git diff main...HEAD -- <ruta>`; con menos de 1,500 líneas basta un `git diff main...HEAD` completo.\nCorrígelo y reporta APPROVED otra vez."
  },
  "next": null
}
```

El comando sale con código 2. En modo texto se imprime `RECHAZADO: <reason>`. Si hay más de 20 archivos pendientes, la lista muestra 20 y agrega `- y N más`.

**Qué tiene que hacer el consumidor**

- Si tienes flujos o automatizaciones que reportan `APPROVED` como reviewer, asegúrate de que el reviewer abra todos los archivos de código y de pruebas del diff (con Read, `git diff <base>...HEAD -- <ruta>` o el diff completo si es chico) antes de reportar.
- Si manejas el código de salida 2 de `bflow report`, considera `review_incomplete` como un rechazo recuperable: abre lo que falta y reporta de nuevo.
- Si tu código lee `data.reason`, no asumas un texto fijo: la razón puede combinar varios bloques (🔴 sin abrir, archivos pendientes, formato del review-map).

## Nuevo campo `pending` en las estadísticas

**Cambio.** `data.stats.review.coverage` incluye ahora `pending`, la lista de archivos obligatorios sin abrir, en el orden del diff.

```json
"coverage": {
  "pending": ["internal/c.go", "internal/c_test.go"]
}
```

`pending` se omite si no falta ningún archivo o si la cobertura no se midió.

**Retrocompatibilidad**

Los eventos anteriores sin `pending` se leen igual: las estadísticas y las pruebas existentes siguen funcionando.

## Guía del reviewer

El archivo del agente reviewer ahora dice que bflow exige abrir todo el código y las pruebas del diff (salvo documentación, `specs/`, lockfiles y binarios) antes de aceptar `APPROVED`. Vuelve a correr `bflow render` para regenerarlo.

## Checklista

- [ ] Si automatizas reportes del reviewer, verifica que abran todo el código y las pruebas del diff antes de `APPROVED`.
- [ ] Si manejas el código de salida 2, trata `review_incomplete` como recuperable.
- [ ] Si lees `data.stats.review.coverage`, tolera que `pending` esté ausente.
- [ ] Corre `bflow render` para actualizar el archivo del reviewer.
