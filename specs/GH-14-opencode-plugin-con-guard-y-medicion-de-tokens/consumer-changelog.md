# Cambios para consumidores · GH-14

OpenCode recibe ahora el mismo guard y medición de tokens que Claude Code, mediante un plugin que `bflow render` genera automáticamente.

## Plugin automático para OpenCode

**Cambio.** Cuando tu `bflow.yaml` incluye `opencode` en la lista de `agent:`, `bflow render` ahora escribe `.opencode/plugins/bflow.js` (se commitea, no se edita).

**Antes (sin cambios en OpenCode)**

```yaml
# bflow.yaml
agent:
  - claude  # solo Claude Code tenía guard y tokens
```

El adaptador de OpenCode no generaba nada; no había guard ni conteo de tokens.

**Ahora (plugin automático)**

```yaml
# bflow.yaml
agent:
  - claude
  - opencode  # ahora OpenCode también genera plugin con guard y tokens
```

`bflow render` escribe:
```
.opencode/
  plugins/
    bflow.js       ← plugin generado (se commitea, no edites)
```

**Qué tiene que hacer el consumidor**

1. Si ya tienes `opencode` en `agent:` en bflow.yaml, corre `bflow render` para generar el plugin.
2. Haz commit y push de `.opencode/plugins/bflow.js`:
   ```bash
   bflow render
   git add .opencode/plugins/bflow.js
   git commit -m "chore: plugin de bflow para OpenCode"
   git push
   ```
3. El plugin se ejecuta automáticamente la próxima vez que OpenCode se lance.

## Guard en OpenCode

**Cambio de comportamiento.** Con el plugin, OpenCode ahora tiene las mismas protecciones que Claude Code: `git reset --hard`, push forzado, `.env` y `.bflow/` están bloqueados en el hook del guard.

**Antes (sin guard)**

Un agente en OpenCode podía correr cualquier comando sin restricciones:
```bash
git reset --hard HEAD~1  # permitido (sin guard)
echo "secrets" > .env    # permitido
rm -rf .bflow/          # permitido
```

**Ahora (con guard del plugin)**

El plugin detiene estas operaciones antes de que ocurran:
```bash
git reset --hard HEAD~1
# Error: guard bloqueó: resets_and_forces
# git reset --hard rompe la trazabilidad de la tarea...
# Termina con `bflow report` después de revisar el diff.
```

Igual con push forzado, archivos sensibles (`.env`, `.bflow/`) y git checkout de ramas protegidas. Ver la columna OpenCode en la tabla de garantías del README para detalles.

**Limitación sin workaround**

OpenCode corre solo las herramientas permitidas por el guard de bflow (bash, edit, write, multiedit, patch, apply_patch). Otras (read, grep, search) no lanzan el guard.

## Medición de tokens en OpenCode

**Cambio.** El plugin mide los tokens de cada respuesta de OpenCode y se los atribuye por fase, agente y modelo en `bflow stats`.

**Antes (sin medición)**

OpenCode solo medía tiempo (no tokens):
```bash
bflow stats GH-14
# Fases: 0m1s agente, 0m30s humano
# Tokens: no disponibles
```

**Ahora (con medición)**

Los tokens se miden igual que en Claude Code:
```bash
bflow stats GH-14
# Fases: 0m1s agente, 0m30s humano
# Tokens: 50K input, 10K output
#   - main: 30K input
#   - bflow-implementer: 20K input (se muestran sin prefijo "bflow-")
```

Los tokens se atribuyen por sesión (principal o subagente) y modelo (proveedor/modelo).

## Lo que OpenCode NO cubre

**Limitación permanente.** OpenCode no tiene evento de fin de subagente ni hook de inicio de sesión; por eso no tiene:

- **Nudge de subagente que termina sin reportar**: En Claude Code, si un subagente termina sin correr `bflow report`, Claude lo detecta y te advierte. OpenCode no tiene ese evento, así que tampoco el nudge. Termina tu parte con `bflow report` explícitamente.
  
- **Hook de inicio de sesión**: En Claude Code, al iniciar cada sesión se cargan compromisos y contexto del stack. OpenCode no lo hace automáticamente. Úsalo como contexto inicial en el prompt del agente o en la descripción de la tarea.

Estos gaps los cubren:
1. `bflow report --verdict DONE|NEEDS_DECISION|BLOCKED` — di explícitamente cuándo terminas.
2. `bflow check --verify` — valida que todo esté en regla antes de mergear.
3. El bloque de AGENTS.md que `bflow render` mantiene — documentación de cada agente.

## Cambios en comandos CLI

**Sin cambios para el consumidor.** Los nuevos flags `--tool` en `bflow guard` y `bflow hook tokens` son internos (usados por el plugin); tu uso del CLI sigue igual.

## Checklista para empezar

Si quieres usar OpenCode con bflow:

- [ ] Agrega `opencode` a la lista de `agent:` en bflow.yaml
- [ ] Corre `bflow render` para generar el plugin
- [ ] Haz commit de `.opencode/plugins/bflow.js`
- [ ] En cada sesión de OpenCode, termina tu trabajo con `bflow report --verdict DONE` (o `NEEDS_DECISION` si la próxima decisión es del humano)
- [ ] Revisa `bflow stats <ID>` para ver tokens por fase y agente
- [ ] Si usas subagentes, sé explícito en tus compromisos: `bflow report` es tu responsabilidad, no un nudge automático

## Excepciones y troubleshooting

### El plugin no se ejecuta

- Corre `bflow doctor` — te dirá si el plugin está ok, falta o difiere.
- Si bflow no está en el PATH, verás un aviso en OpenCode: "bflow: no está en el PATH; el guard no está activo".

### Salida diferente en bflow stats

- Los tokens son locales a tu repo y sesión. Si regeneras la sesión, se pierden.
- Con `agent:` que incluye varios agentes, cada uno tiene su rama y plugin.

### Cambié bflow.yaml pero no se regenera el plugin

- Corre `bflow render` explícitamente; el plugin se actualiza cuando cambias `agent:` o subagentes.

