# Changelog: GH-28 · guard desenvuelve bash -c

## Resumen

No hay cambios que rompan compatibilidad. Este cambio es interno a bflow y afecta solo la precisión de la lógica del guard:

- El guard ahora evalúa las reglas de Bash sobre el comando que está dentro de `bash -c`, no solo sobre la envoltura.
- La medición de las lecturas del reviewer es más exacta cuando usa shells con `-c` (mejora a GH-20, no regresión).

## Sin cambios en lo que consumen otros equipos

- No hay cambios en endpoints, campos, enums o errores.
- No hay cambios en permisos o comportamiento visible.
- No hay API pública modificada.
- El change es interno a `internal/guard/guard.go` y es una mejora en la precisión.

## Efecto en el flujo

**Guard más estricto (solo endurece, no relaja):**
- Un agente subagente que intente `bash -c "bflow approve X"` será negado con `human_only` (antes: pasaba sin verificación si no se ve el subcomando).
- Los mismos comandos peligrosos (`git reset --hard`, `git push -f`, etc.) ahora se detectan aunque estén dentro de `bash -c`.

**Medición de lecturas más exacta:**
- Cuando el reviewer corre `bash -c "cat archivo.md"`, ahora se cuenta como lectura de `archivo.md` (antes: se contaba como lectura de "bash").
- Esto es más preciso, no menos. Ver changelog de GH-20 para contexto.
