# Changelog para consumidores: GH-165 (escáner de comentarios narrativos)

## Cambio de comportamiento: menos rechazos por `narrative_comment`, y se revisan líneas `++ ` que antes se perdían

`bflow report <ID> --agent implementer --verdict DONE` y `--agent documenter --verdict DONE`, y `bflow check --verify`, usan la misma regla de GH-151. Este cambio ajusta qué cuenta como comentario y qué líneas agregadas se revisan. No cambia la forma de la salida JSON, ni los códigos de salida, ni la configuración.

No es un cambio que rompa compatibilidad: puede bajar o subir los rechazos según el diff, así que conviene revisar un DONE que cambie de resultado.

### Qué deja de rechazarse

- Un `//` dentro de comillas dobles, simples o backticks de la misma línea (`s := "a // T3"`) ya no cuenta como comentario, aunque cite el ID de la tarea o `T<n>`.
- Una directiva del preprocesador pegada al `#` (`#include`, `#define`, `#pragma`, `#endif`, `#if`… en C y C++) ya no cuenta como comentario `#`.

### Qué se revisa ahora

- Una línea agregada cuyo texto empieza con `++ ` (por ejemplo `++ T3 listo`) antes se confundía con la cabecera del archivo y se perdía, junto con las líneas siguientes de ese archivo. Ahora se revisa. Si cita el ID de la tarea, `R<n>`, `T<n>`, la spec, «según», «decisión» o el walkthrough, el DONE sale con código 2 y `narrative_comment` como cualquier otra línea.

### Qué tiene que hacer el consumidor

- Nada, salvo que un DONE cambie de resultado. Si ahora sale con `narrative_comment` por una línea `++ ` que antes pasaba, quitar o reescribir el comentario como explicación del por qué, commitear y volver a reportar DONE. Lo de la tarea, la spec o lo validado va en `reports/impl.md` y en el PR.
- Si un CI trata `check --verify` como fallo cuando sale `not_verified`, sigue igual; el cambio solo hace que sea `not_verified` en menos casos por comillas y directivas y en más casos por líneas `++ `.
