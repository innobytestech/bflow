# Changelog para consumidores: GH-173

## Rompe compatibilidad

- **Una prueba T1 cuenta como roja solo con su nombre exacto.** `bflow check` y CONTRACT_READY ya no aceptan coincidencias parciales.
  - Go: solo `--- FAIL: <Func>` con límite (espacio, fin de línea o `/` para subpruebas). `TestSum` no coincide con `TestSuma`.
  - Otros stacks: la ruta completa del archivo T1 con límites. `a_test.go` no coincide con `ba_test.go`, y el nombre base solo no cuenta.
  - Qué hacer: si tu repo dependía de una coincidencia parcial para que CONTRACT_READY avanzara, la respuesta ahora es `contract_green`. Ajusta la salida o el nombre de la prueba para que la ruta o `--- FAIL: <Func>` aparezca completa.

## Cambios de comportamiento

- **Un error de compilación o de setup abre la gate `contract` en vez de rechazar.** Si el paso de pruebas del último check contiene `[build failed]` o `[setup failed]`, CONTRACT_READY devuelve `verified=false` sin rechazo y la tarea queda en `contract` con la gate abierta. Antes podía rechazarse con `contract_green` o congelar una prueba rota.
  - Qué hacer: la persona de la gate `contract` decide si acepta el contrato o pide cambios. No hay código de error nuevo.
- **Sin validación del contrato, la gate `contract` se anuncia antes de llegar.** Cuando no hay checker, o `check.test_step` no está en `check.steps`, `bflow watch` y `bflow ui` marcan `contract` como parada humana, y `Upcoming` devuelve la gate `contract` tras `contract`. Antes decían `implementing`.
  - La salida JSON de `bflow status` no cambia.
- **`pause` sobre una tarea bloqueada nombra la fase real.** El error `pause_late` decía `fase blocked`; ahora dice la fase donde se bloqueó (por ejemplo, `fase quality`). El código del error no cambia.
- **Pausar una tarea retirada responde `task_dropped`.** La spec de GH-153 pedía `pause_late`; el código ya daba `task_dropped` y ahora es el único resultado.
- **Congelado de pruebas después de la transición.** Si la transición de `contract` a `implementing` se rechaza, las pruebas no quedan congeladas. Si el congelado falla, la tarea queda en `contract` con la gate abierta y un aviso. Si falla un efecto posterior, las pruebas congeladas vuelven a su estado previo.
  - Qué hacer: nada. Tras un rechazo de CONTRACT_READY las pruebas ya no quedan congeladas.

## Sin cambios

- Comandos, flags, configuración y formato de `frozen-tests.json`.
