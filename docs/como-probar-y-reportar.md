# Cómo probar la rc.9 y reportar issues

Gracias por probar bflow. Es una prerelease (v0.1.0-rc.9): hay cosas que fallan, y lo más útil que puedes hacer es contarnos cuáles.

## Qué esperamos de ti

1. **Instala** ([instalación](instalacion.md)) y corre `bflow version` y `bflow doctor`.
2. **Recorre una tarea con el tracker local**, sin conectar nada, en un repo de prueba:

   ```bash
   cd mi-repo
   bflow init
   bflow install claude          # o: bflow install opencode
   bflow new --lane light --title "Una tarea pequeña"
   ```

   Después abre tu agente y dile "qué sigue". Cada respuesta de bflow dice el siguiente paso; sigue hasta el PR.
3. **Repite con tu tracker** (Plane o GitHub, ver [configuración](configuracion.md)) y con el agente que usas de verdad.
4. Apunta lo que te sorprendió: un mensaje confuso, un paso que no entendiste, algo que tuviste que preguntar.

Para entender qué esperar en cada paso, lee la [guía](guia.md) y los [conceptos](conceptos.md).

## Cómo reportar un issue útil

Abre un issue en <https://github.com/innobytestech/bflow/issues> con:

- La salida de `bflow version`.
- La salida de `bflow doctor`.
- Qué agente (Claude Code u OpenCode, y su versión) y qué tracker usas.
- Tu sistema operativo.
- Los pasos, en orden, hasta el problema, y qué esperabas que pasara.
- La salida completa del comando que falló. Si salió con código 2, repítelo con `--json` y pega todo: trae el motivo y el `next`.

No pegues tokens ni el contenido de `.env`. Si el problema es de una tarea, `bflow show <ID> check` y `bflow stats <ID>` ayudan.

## Problemas conocidos de la rc.9

Antes de reportar, revisa si ya está:

- [#39](https://github.com/innobytestech/bflow/issues/39): guard no reconoce ejecutables entre comillas con espacios en la ruta.
- [#52](https://github.com/innobytestech/bflow/issues/52): el panel sigue con el binario viejo tras `go install` o `bflow update`.
- [#53](https://github.com/innobytestech/bflow/issues/53): el contexto final queda ~0.8k abajo de Claude Code (salida de la última llamada incompleta).
- [#54](https://github.com/innobytestech/bflow/issues/54): no hay forma de regresar a `implementing` desde `documenting` (`reject` da `no_gate`).
- [#55](https://github.com/innobytestech/bflow/issues/55): el documenter omite el CHANGELOG cuando cambia la salida pública.
- [#84](https://github.com/innobytestech/bflow/issues/84): los tokens de la sesión principal a mitad de turno no se ven en el panel hasta el Stop.
- [#86](https://github.com/innobytestech/bflow/issues/86): un agente puede editar pruebas congeladas desde Bash (python, sed, heredoc) sin que guard lo bloquee. Las pruebas congeladas se siguen revisando al reportar DONE.
- [#91](https://github.com/innobytestech/bflow/issues/91): guard: un `<<EOF` entre comillas oculta las líneas siguientes como cuerpo de heredoc.

La lista completa está en los [issues abiertos](https://github.com/innobytestech/bflow/issues).
