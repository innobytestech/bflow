# Consumer changelog v0.1.0

## Breaking changes

### Reviewer approval now requires opening all code and tests in the diff

**Before:** `bflow report --agent reviewer --verdict APPROVED` accepted a review if all files marked with 🔴 in the review-map were read. Files marked 🔴 by the reviewer, so the bar was determined by the reviewer themself. Test files could be skipped.

**After:** `bflow report --agent reviewer --verdict APPROVED` rejects if any code or test file remains unread, except:
- Docs (files matching `review.IsDoc`)
- Files under `specs/`
- Known lockfiles: `go.sum`, `go.work.sum`, `package-lock.json`, `npm-shrinkwrap.json`, `pnpm-lock.yaml`, `bun.lockb`, or any `*.lock`
- Binary files detected by git (`git diff --numstat`)

A file marked for deletion is counted as read if accessed with `git diff -- <ruta>` or `git show -- <ruta>` after `--`, even if it no longer exists on disk.

**Error response:**
```
{
  "status": "BLOCKED",
  "error": "review_incomplete",
  "message": "Archivos del diff (código y pruebas) que no abriste:\n- path/to/file1.go\n- path/to/file2_test.go\n- y 3 más\nÁbrelos con Read o `git diff <base>...HEAD -- <ruta>` [; con menos de 1,500 líneas basta un `git diff <base>...HEAD` completo]."
}
```

**What you need to do:**
1. Ensure all tests explicitly open the code and test files they create in the diff, using Read, `git diff -- <ruta>`, or a complete `git diff <base>...HEAD` for small diffs (< 1,500 lines).
2. Update test workflows that mock the reviewer: add reads for all non-exempt files, or mock `review_coverage` to indicate they were read.
3. If a reviewer approval previously worked by marking only 🔴 files without test reads, that flow now rejects and requires opening tests.

**Field change:**
- `data.stats.review.coverage`: new optional field `pending` (list of unread required files). Empty or omitted if all required files are read or coverage is not measured.
  - Existing events without `pending` are read the same way: old tests and stats continue to work.

## Other changes

- Deleted or renamed files are now tracked as read if accessed with pathspec (after `--` in git commands), allowing full coverage on diffs with removals without needing `git show` on the old content.
- Reviewers see updated guidance in the agent file: "bflow requires opening all code and test files in the diff (except docs, `specs/`, lockfiles, and binaries) before APPROVED is accepted."
