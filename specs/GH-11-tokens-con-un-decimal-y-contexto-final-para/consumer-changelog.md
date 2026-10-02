# Consumer Changelog: GH-11

## Summary
Tokens displayed in `bflow watch`, the panel, and `bflow metrics` are now rounded to one decimal place and include context final (the last call of each agent: input + cache + output), matching Claude Code's agent map.

## Tokens format

**Before:** `15k`, `140k`  
**After:** `15.9k`, `140.0k`

Thousands are rounded to one decimal, millions follow the same pattern (`1.2M`). This affects:
- New tokens
- Cache tokens
- Max context
- All output using the `Human` formatter (including `bflow setup` and `bflow doctor`)

## Context final display

A new metric shows the context of the last call for each agent (input + cache read + cache write + output), labeled "final" in `bflow watch` and the panel.

**watch and panel output:**
```
<new-tokens> nuevos · <n> llamadas de hasta <max-context> · final <last-context>
```

Example: `14.0k nuevos · 2 llamadas de hasta 140.0k · final 15.9k`

**bflow metrics table:**
New column `ctx final` shows the last call context for each agent/model combination; rows without data show `-`.

## What doesn't change
- `bflow calls` continues to show exact token counts
- The definition of "new" and "cache" tokens remains the same
- Only displays context final for tasks registered after this change; older tasks show nothing (or `-` in the table)
