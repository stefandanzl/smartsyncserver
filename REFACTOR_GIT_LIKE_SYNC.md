# Refactor: Git-Like Sync (Remove Directory Tracking)

## Goal

Convert to git-like sync: track only files, not directories. Remove directory tracking (pseudo-hashes, trailing slashes).

**Directories created implicitly by `os.MkdirAll` when files need them — no tracking needed.**

---

## Changes to Make

### 1. `storage/checksums.go`

**DELETE `AddDir()` method entirely** (lines 248-259)

**In `Scan()` method (lines 190-198)**: DELETE the `if info.IsDir()` block that adds `newData[relPath+"/"] = "dir"` and `matcher.MatchDir()` call

**In `Rename()` method**:
- DELETE directory normalization logic (lines 138-146) — trailing slash checks, `fromPath + "/"` logic
- DELETE nested path renaming loop (lines 155-161) — loop that renames children

**In `Delete()` method**:
- DELETE trailing slash normalization
- DELETE nested path deletion loop (lines 125-129) — loop that deletes children

### 2. `handlers/handlers.go`

**DELETE these methods entirely:**
- `handleCreateFolder()` (lines 258-269)
- `handleDeleteFolder()` (lines 271-289)
- `handleFolder()` (lines 230-256) — the `/folder/*` route dispatcher

**File handlers (CreateFile, DeleteFile, RenameFile)**: Keep as-is. They already update checksums efficiently (O(1) operations).

### 3. `storage/vault.go`

No changes needed. `PutFile()` and `Rename()` already call `os.MkdirAll` for parent directories.

---

## Result

- `checksums.json` contains only files (no "dir" entries, no trailing "/")
- No `/folder/*` endpoints
- File operations still update checksums immediately (cheap single-file operations)
- Full scans work for complete rebuilds
