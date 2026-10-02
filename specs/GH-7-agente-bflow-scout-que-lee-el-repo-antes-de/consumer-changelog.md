# Consumer changelog · GH-7

## New: `bflow report` accepts `--stdin` for scout agent

The scout agent can now report using standard input, which bflow will save to `.bflow/tasks/<id>/reports/scout.md`.

**Usage:**
```bash
bflow report <id> --agent scout --verdict DONE --stdin < report.txt
```

Or with a heredoc:
```bash
bflow report <id> --agent scout --verdict DONE --stdin <<'EOF'
- File: pkg/handler.go (40 lines)
- Tests: handler_test.go with 12 test cases
- Related: pkg/middleware.go (used in 3 functions)
EOF
```

**Constraints:**
- `--stdin` is exclusive to the scout agent; other agents cannot use it
- Report content must be non-empty and ≤40 lines
- Exit code 2 with reason if constraints are violated

**Error responses (exit code 2):**
- `scout_empty`: Content is empty or only whitespace
- `scout_too_long`: More than 40 lines (includes line count and maximum)
- `stdin_scout_only`: Non-scout agent tried to use `--stdin`

## New: `bflow show <id> scout` command

Read the scout report for a task.

**Usage:**
```bash
bflow show <id> scout
```

**Response:**
- If report exists: the report content (max 40 lines)
- If report does not exist: `(<id> no tiene reporte del scout)` (exit code 0, not an error)

**In JSON output:**
The `show` command includes `scout` in the list of available artifacts.

## Impact for consumers

- **CI/automation:** You can now consume scout reports programmatically via `bflow show <id> scout`
- **Agents:** Scout is a reserved agent name; custom agents cannot use this name
- **Leaders:** Use `bflow show <id> scout` to pre-populate context before invoking discovery or spec agents
