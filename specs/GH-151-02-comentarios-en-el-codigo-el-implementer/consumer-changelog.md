# Changelog para consumidores: GH-151 (comentarios narrativos)

## Cambio de comportamiento: DONE puede rechazarse por `narrative_comment`

`bflow report <ID> --agent implementer --verdict DONE` y `--agent documenter --verdict DONE` ahora rechazan el reporte si una línea de comentario agregada en el diff contra la base (`base...HEAD`) dentro de `check.code_paths` cita el ID de la tarea, `R<n>`, `T<n>`, `spec`, `según`, `decisión` o `walkthrough`. Los archivos `.md` no se revisan. Las líneas que ya estaban en el código no cuentan: solo las agregadas.

No cambia la forma de la salida JSON ni los códigos de salida existentes; aparece un código de rechazo nuevo.

### Rechazo nuevo

- Código de rechazo: `narrative_comment`. El comando sale con código 2.
- Campo `reason`: lista hasta 20 hallazgos con el formato `ruta:línea: texto del comentario`, seguida de `y N más` si hay más. Termina con la instrucción de qué hacer.
- Qué tiene que hacer el consumidor (agente o persona): quitar o reescribir el comentario como explicación del por qué, commitear y volver a reportar DONE. Lo de la tarea, la spec o lo validado va en `reports/impl.md` y en el PR.

### `bflow check --verify`

- Si el diff tiene comentarios narrativos, el resultado es `not_verified` (antes, con el mismo check válido, salía verificado).
- El campo `detail` pasa a `comentarios narrativos: ruta:línea, ...` (ruta y línea de cada hallazgo, separados por coma).
- Quien consume `check --verify` en CI debe tratar `not_verified` como fallo, como ya lo hace con las pruebas congeladas.

### Configuración nueva (opcional)

Claves nuevas bajo `flow` en `bflow.yaml`. Sin ellas, el comportamiento es el de la regla por defecto.

- `flow.narrative_comments` (booleano, por defecto `true`). `false` apaga la regla.
- `flow.comment_patterns` (lista de expresiones regulares, por defecto vacía). Si trae valores, reemplaza los patrones fijos. El ID de la tarea se busca siempre. Una lista vacía vuelve a los patrones fijos; no apaga la regla.
- Una expresión regular inválida hace que la configuración no cargue: todo comando falla y nombra el patrón.

Ejemplo:

```yaml
flow:
  comment_patterns: ['\bR\d+\b', '\bT\d+\b', '(?i)\bwalkthrough\b']
  # narrative_comments: false
```

### Qué no cambia

- Firmas de comandos, flags y el resto de códigos de rechazo.
- Un DONE con comentarios que explican un por qué (sin los patrones) sigue pasando.
- Si el diff contra la base no se puede calcular (sin base o git falla), la regla no rechaza: el DONE sigue su curso.
