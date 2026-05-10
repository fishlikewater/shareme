---
name: record-session
description: "Record work progress after the agreed executor has tested and committed code"
---

[!] **Prerequisite**: This skill should only be used AFTER the agreed executor has tested and committed the business changes, or when this is a planning / documentation-only session.

**Do NOT run `git commit` directly** — the scripts below handle their own commits for `.trellis/` metadata. You only need to read git history (`git log`, `git status`, `git diff`) and run the Python scripts.

If the task used `docs/superpowers/plans/*.md`, make sure that plan is aligned with the final implementation state before archiving or recording the session.

---

## Record Work Progress

### Step 1: Get Context & Check Tasks

```powershell
python .\.trellis\scripts\get_context.py --mode record
```

[!] Archive tasks whose work is **actually done** — judge by work status, not the `status` field in task.json:
- Code committed? → Archive it (don't wait for PR)
- All acceptance criteria met? → Archive it
- Don't skip archiving just because `status` still says `planning` or `in_progress`

```powershell
python .\.trellis\scripts\task.py archive <task-name>
```

### Step 2: One-Click Add Session

```powershell
# Method 1: Simple parameters
python .\.trellis\scripts\add_session.py `
  --title "Session Title" `
  --commit "hash1,hash2" `
  --summary "Brief summary of what was done"

# Method 2: Pass detailed content via stdin
@'
| Feature | Description |
|---------|-------------|
| Change | Description |
|--------|-------------|
| API | Updated endpoint contract |
| UI | Synced client interaction |

**Updated Files**:
- `src/module/example-file.ext`
- `docs/spec/example.md`
'@ | python .\.trellis\scripts\add_session.py --title "Title" --commit "hash"
```

**Auto-completes**:
- [OK] Appends session to journal-N.md
- [OK] Auto-detects line count, creates new file if >2000 lines
- [OK] Updates index.md (Total Sessions +1, Last Active, line stats, history)
- [OK] Auto-commits .trellis/workspace and .trellis/tasks changes

---

## Script Command Reference

| Command | Purpose |
|---------|---------|
| `python ./.trellis/scripts/get_context.py --mode record` | Get context for record-session |
| `python ./.trellis/scripts/add_session.py --title "..." --commit "..."` | **One-click add session (recommended)** |
| `python ./.trellis/scripts/task.py archive <name>` | Archive completed task (auto-commits) |
| `python ./.trellis/scripts/task.py list` | List active tasks |
