# Changelog para consumidores · GH-153

## Rompe compatibilidad

- **El carril full ya no abre la gate `contract` ni la gate `pause` por defecto.** Un flujo full pasa de 7 a 5 gates humanas: carril, discovery, spec, preguntas de producto y walkthrough. Qué hacer: si tu script, tablero o procedimiento espera una gate `contract` o `pause` en el full, quítalo. Aprobar `--gate contract` o `--gate pause` sobre una tarea que no está esperando esa gate sale con error.
- **`paused` sale de los carriles por defecto del full.** La fase `paused` solo existe si la declaras en `lanes.full` de `bflow.yaml`. Un `lanes.full` explícito que ya la incluya la conserva.

## Cambios

- **Validación automática del contrato.** Al reportar `CONTRACT_READY` del implementer en la fase `contract`, bflow valida el contrato con el último `bflow check <ID>` guardado, sin correr pruebas.
  - Si pasa, congela las pruebas T1 y la tarea avanza a `implementing` sin gate, con el `resume` activo para el implementer.
  - Si no pasa, `report` rechaza con código 2 y uno de estos códigos (la tarea no avanza):
    - `contract_stale`: no hay check, el check es `DIRTY`, o hay código de `check.code_paths` cambiado después del check. Qué hacer: commitear el contrato, correr `bflow check <ID>` y reportar otra vez.
    - `contract_broken`: un paso de `check.steps` anterior a `check.test_step` terminó en `fail`. Responde con el nombre del paso y hasta 10 líneas de fallo. Qué hacer: corregir lo previo a las pruebas, commitear, correr `bflow check <ID>` y reportar otra vez.
    - `contract_green`: `check.test_step` pasó, o falló sin ninguna línea que nombre una prueba T1. Qué hacer: que las T1 fallen contra los stubs, commitear, correr `bflow check <ID>` y reportar otra vez.
  - Si bflow no puede validar (sin checker, `check.steps` vacío o `check.test_step` fuera de los pasos), abre la gate `contract` como antes. Al aprobarla, las pruebas se congelan.
  - `contract_hollow` no cambia: las pruebas huecas siguen rechazadas.
- **`bflow pause <ID>` (nuevo).** Pide parar antes de `quality`. Al terminar `implementing` con DONE y check en verde, la tarea entra a `paused` y abre la gate `pause`. Aprobarla lleva a la fase siguiente a `paused` en el carril; rechazarla vuelve a `implementing`.
  - Se puede repetir sin error.
  - Rechaza con `pause_late` si la tarea ya llegó a `quality` o una fase posterior, o está bloqueada (`blocked`); con `task_dropped` si se retiró; con `not_started` si no ha empezado.
  - Solo una persona lo puede correr; el agente no.
  - Si no se corre, el DONE de `implementing` pasa directo a `quality`.
- **`next`, `watch`, `ui` y las notificaciones no anuncian gates que no van a abrirse.** En el full, `contract` y `pause` no aparecen como próxima decisión humana salvo en los casos de arriba. En `implementing`, `paused` se anuncia solo si hay marca de pausa.
- **Texto de la fase `contract`.** El oficio del implementer dice que `CONTRACT_READY` exige el contrato commiteado y un `bflow check <ID>` con las T1 en rojo. Se regenera con `bflow render`.

## Qué tiene que hacer el consumidor

1. Quitar de tus flujos la espera de la gate `contract` y `pause` en el full.
2. Si usas `bflow pause`, correrlo antes de que la tarea llegue a `quality`.
3. Si manejas `CONTRACT_READY` fuera de bflow, manejar los tres códigos nuevos (`contract_stale`, `contract_broken`, `contract_green`) como rechazos de `report`.
4. Correr `bflow render` en cada repo para regenerar los oficios.
