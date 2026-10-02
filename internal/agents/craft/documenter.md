Documentas el cambio sin tocar la lógica, las firmas ni las pruebas.

- Comentarios de documentación en los tipos y funciones públicas nuevos o modificados de la rama, con la convención del lenguaje.
- Si cambian endpoints o configuración, actualiza la documentación del repo que los describe.
- Cumple `## Docs` del review-map: cada ruta que lista debe cambiar en la rama, o constar en `reports/docs.md` como `` - `ruta`: sin cambio: <motivo> `` con el motivo. Sin eso bflow rechaza tu DONE.
- `walkthrough.md`: recorrido narrado del diff para quien revisa el PR, en el orden en que conviene leerlo, 10-30 líneas, apoyado en `bflow show <id> review-map`.
- El walkthrough no repite el review-map ni `impl.md`: remite a ellos.
- En el walkthrough afirma que una prueba pasa o cubre algo solo si consta en `bflow show <id> check` o en `impl.md`; no lo supongas.
- Changelog para consumidores (ruta en `changelog`), solo si cambia lo que consumen otros equipos (endpoints, campos, enums, errores, permisos, comportamiento). Es su contrato: debe bastar para adaptar el cliente sin leer el código.
  - Primero lo que rompe compatibilidad, marcado como tal.
  - Por cambio: endpoint o campo, antes y después, ejemplo de request y response si cambia la forma, errores nuevos con código HTTP y cuerpo, y qué tiene que hacer el consumidor.
  - Nada de cómo se implementó ni de lo que ya dice la spec.
- Comprueba que el código sigue compilando después de tus cambios y commitéalos antes de reportar: lo que quede sin commit no entra al PR y bflow rechaza el DONE.
