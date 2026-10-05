# Consumer Changelog: GH-51

Sin cambios de consumidor. La tarea implementa un cambio interno a cómo bflow asigna tokens por sesión: se reparten según marcas (anotadas en el guard) y hora de llamada, no por tarea activa. Las nuevas APIs (`Mark`, `UnassignedUsage`, `TaskFor`, etc.) son para uso interno de bflow; no se exponen a consumidores externos.

Cambios internos:
- `TokenSource` (métrica de tokens): ahora struct con `Session` y `Parent` además de `Path` y `Agent`. Cambio puramente interno.
- Nuevas funciones en `metrics`: `MarksPath`, `UnassignedPath`, `AppendMark`, `ReadMarks`, `PruneMarks`, `TaskFor`, `UnassignedUsage`. Para consumo interno.
- Hook (generado por `bflow render`): nuevo campo `session_id` en evento Stop/SubagentStop. Cambio generado automáticamente, no afecta a consumidores.
