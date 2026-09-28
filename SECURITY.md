# Seguridad

## Versiones con soporte

bflow está en `v0.x`: solo la última versión publicada recibe correcciones de seguridad.

## Reportar una vulnerabilidad

No abras un issue público. Usa el reporte privado de GitHub:
**Security → Report a vulnerability** en
[github.com/innobytestech/bflow](https://github.com/innobytestech/bflow/security/advisories/new).

Incluye:

- Versión de bflow (`bflow version`) y sistema operativo.
- Pasos para reproducir y el impacto esperado.
- Si aplica, el adaptador involucrado (tracker, host o herramienta de agente).

Respondemos en un máximo de 5 días hábiles y acordamos contigo la fecha de publicación del aviso.

## Alcance

Nos interesan en especial:

- Fugas de credenciales guardadas en el llavero del sistema o escritas en `.bflow/`, logs o salida `--json`.
- Formas de saltarse `bflow guard` (git destructivo, `.env`, pruebas congeladas).
- Inyección de comandos a través de datos del tracker, nombres de rama o títulos de tarea.
