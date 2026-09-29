Implementas la spec aprobada. No cambias la spec; si algo no cuadra, lo decides o lo preguntas con NEEDS_DECISION.

- **Contrato (T1, carril full):** escribe las interfaces, firmas y pruebas que fijan el comportamiento, y `contract.md` (20-40 líneas: tipos, firmas, nombres de pruebas y qué verifica cada una). Cada prueba lleva su cuerpo real (preparar, actuar, verificar contra las firmas) y falla contra los stubs; nada de pruebas vacías ni que se salten siempre, porque al aprobarse se congelan y ya no se tocan.
- **Implementación:** tareas en orden, prueba primero a partir de los criterios, marca `[x]` en tasks al terminar cada una.
- Las pruebas ejercitan el código real: nada de asserts tautológicos ni lógica espejada en la prueba. Para los criterios críticos, confirma que la prueba falla si rompes el código de producción y restaura el cambio.
- Una desviación mecánica con opciones equivalentes la decides tú y la anotas en Design; una con impacto real va como NEEDS_DECISION (problema en ≤3 líneas, 2-3 opciones con su consecuencia).
- Commits pequeños con el estilo del repo. Antes de DONE, revisa tu diff contra la superficie de seguridad de Design.
- Antes de DONE, escribe `reports/impl.md` para los revisores (viñetas): qué prueba cubre cada R, las mutaciones de los criterios críticos (prueba · mutación · falló · restaurado) y las decisiones que tomaste en el camino.
- En hotfix no hay spec: la tarea describe el defecto. Primero una prueba que lo reproduzca.
