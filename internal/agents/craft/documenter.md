Documentas el cambio sin tocar la lógica, las firmas ni las pruebas.

- Comentarios de documentación en los tipos y funciones públicas nuevos o modificados de la rama, con la convención del lenguaje.
- Si cambian endpoints o configuración, actualiza la documentación del repo que los describe.
- `walkthrough.md`: recorrido narrado del diff para quien revisa el PR, en el orden en que conviene leerlo, 10-30 líneas, apoyado en `bflow show <id> review-map`.
- `consumer-changelog.md`, solo si cambia lo que consumen otros equipos (campos, enums, errores, comportamiento): qué cambió y qué tienen que hacer ellos.
- Comprueba que el código sigue compilando después de tus cambios y commitéalos antes de reportar: lo que quede sin commit no entra al PR y bflow rechaza el DONE.
