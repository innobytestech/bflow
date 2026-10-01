# Consumer changelog: tokens por llamada (GH-29)

## New: `stats` command gains `--calls` flag

### What changed

`bflow stats <ID>` now accepts an optional `--calls` flag to display per-call token usage instead of just per-phase aggregates.

### Before
```
$ bflow stats GH-29
GH-29 · implementing · 2h32m (agente 2h30m · humano 2m)
  entrada: 845,600 · output: 28,941 · caché escrita: 18,200 · releído: 344,086 · nuevo: 892,741
```

### After (same command without `--calls`)
Same as before. The flag is optional.

### After (with `--calls` flag)
```
$ bflow stats GH-29 --calls
GH-29 · implementing · 2h32m (agente 2h30m · humano 2m)
  entrada: 845,600 · output: 28,941 · caché escrita: 18,200 · releído: 344,086 · nuevo: 892,741

implementer · implementing · 9 llamadas · contexto final 60,079 · releído 344,086 · nuevo 61,204
   # hora       modelo       entrada  caché escrita  releído  salida  contexto  acum. releído  acum. nuevo
   1 15:04:21  claude-opus  12,345   1,200         5,432    2,100   18,977    5,432         15,645
   2 15:04:35  claude-opus  18,500   2,100         8,932    3,200   29,532    14,364        26,945
   ... (more rows)
 total                       845,600  18,200        344,086  28,941           344,086        892,741
```

A summary shows:
- Per run (agent/session): label, phase, call count, final context, total cached read, total new tokens
- A formatted table with per-call details including accumulating totals

### For API consumers

#### JSON output without `--calls` (unchanged)
```json
{
  "status": "ok",
  "data": {
    "stats": { "id": "GH-29", "phase": "...", ... }
  }
}
```

#### JSON output with `--calls` (new)
```json
{
  "status": "ok",
  "data": {
    "stats": { "id": "GH-29", "phase": "...", ... },
    "calls": [
      {
        "run": "claude:my-transcript",
        "agent": "implementer",
        "label": "implementer",
        "phase": "implementing",
        "rows": [
          {
            "n": 1,
            "ts": "2025-10-01T15:04:21Z",
            "run": "claude:my-transcript",
            "tool": "claude",
            "agent": "implementer",
            "phase": "implementing",
            "model": "claude-opus",
            "msg": "msg-id-123",
            "input": 12345,
            "cache_write": 1200,
            "cache_read": 5432,
            "output": 2100,
            "context": 18977,
            "acc_read": 5432,
            "acc_new": 15645
          },
          ...
        ],
        "total": {
          "input": 845600,
          "output": 28941,
          "cache_read": 344086,
          "cache_write": 18200,
          "calls": 9,
          "max_context": 60079
        },
        "final_context": 60079,
        "last": "2025-10-01T15:42:30Z"
      }
    ]
  }
}
```

### Requirements for consumers

- The `--calls` flag is **optional** and requires an ID (fails with "usage" error if ID is omitted)
- Without the flag, `stats` behavior and JSON output are **unchanged**
- With the flag:
  - CLI output includes per-call tables with formatted numbers (thousands separator: `,`)
  - JSON includes a `calls` array with run objects
  - Each run has `rows` (individual calls) and `total` (aggregate for that run)
  - Numbers in JSON are integers (no decimals); text representation uses formatting

### Notes for legacy tasks

Tasks created before GH-29 (before per-call tracking was added) will show one of:
- "sin detalle por llamada (registrado antes de GH-29)" — if `calls.jsonl` does not exist
- "parte del gasto se registró antes de GH-29" — if some token usage predates call tracking

The `--calls` flag will still work and show available data.

### Web panel

The panel (`bflow watch --web` or `bflow ui`) now displays a "Tokens" section with:
- A collapsible block per run (agent or main session)
- Summary on each block: label, call count, final context, total cached read, total new
- Per-call table inside (details hidden by default, toggled by click)
- Most recent run is open by default; user's open/close choice is remembered per run

No format changes to other panel sections.
