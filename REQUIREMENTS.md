# Obsidian Vault Sync Server — Specification

## 1. Overview

This server provides a minimal HTTP API for syncing an Obsidian vault across multiple devices.
Programming language is Go for small resource footprint and fast speed

Client (Obsidian plugin) handles: - Conflict detection - Change categorization - Initial mirror
It does that by also keeping a similar list of all files aswell as their respective checksums, this way local and remote filechanges immediately are visible - if both would differ from the previous checksums that would be a conflict and resolved by the user.

# 2. Storage Layout

project-root/
├── vault/
│ ├── notes/ ....
│ ├── attachments/ ...
│ └── ...
├── config.yaml
├── checksums.json
└── smartsyncserver.go

### Rules

- All vault files are stored inside `./vault` or whatever is defined
- All paths are relative to `./vault`
- No access outside this directory is allowed
- Path traversal (`../`) must be rejected

---

# 3. Authentication

All endpoints except `/status` require:
Authorization: Bearer [TOKEN]
Token is stored in `config.json`.
If token invalid or missing → return:
401 Unauthorized

---

# 4. Endpoints

## 4.1 Status

### GET `/status`

Returns basic health information.

### Response

{
"online": true,
"file_count": 123,
}

---

# 4.2 Single File Operations

## GET `/file/{path}`

Download a single file.

- Returns raw bytes
- Works for text and binary
- 404 if not found

### Success Response (200)

**Headers:**
HTTP/1.1 200 OK
Content-Type: [detected mime type]
Content-Length: [file size in bytes]
**Body:**
[raw file bytes]
No JSON wrapping.  
No Base64.  
Just the exact file content.

---

### Example (Markdown file)

HTTP/1.1 200 OK
Content-Type: text/markdown
Content-Length: 42

This is the file content.

---

### Example (PNG file)

HTTP/1.1 200 OK
Content-Type: image/png
Content-Length: 183245
Body = raw PNG bytes.

---

## PUT `/file/{path}`

Upload or overwrite a single file.

- Body = raw bytes
- Create directories automatically if needed
- Overwrites existing file

Response:
{
"success": true
}

---

## DELETE `/file/{path}`

Delete a single file
Can also
Response:
{
"deleted": true
}

---

# 📂 Folder Endpoints

## PUT `/folder/{path}`

Create a folder

Behavior:

- Recursive create (mkdir -p style)
- Idempotent (recommended)

Response:
{
"created": true
}

## DELETE `/folder/{path}`

Delete a folder

Recommended behavior:
delete also if not empty!

Response:
{
"deleted": true
}

# 🔁 Rename (File or Folder)

## POST `/rename`

Request:
{
"type": "folder" | "file" ,
"from": "notes/a.md",
"to": "notes/b.md"
}
Works for:

- Files
- Folders

Response:
{
"renamed": true
}

---

# 5. Checksums

Server maintains `checksums.json`
This is the central feature for this project
The used algorythm is sha256 because this is what obisidan uses for its internal filecache, so we almost dont need to calculate any checksums on the clientside which is really efficient
upon every syncronisation all checksums are stored on the client to enable easy change comparison

{
"notes/a.md": "fkdslfoee33trj3efd",
"notes/b.md": "asdf93jhk393jj3kj",
"notes/c.md": "as333df93jhk333j",
....
}
Purpose:

- Fast metadata response
- Avoid full disk scan on every request

Server should update this file whenever:

- A file is uploaded
- A file is deleted
- A file is renamed
  This is dependent on the `scan` setting in `config.yaml`

# 6. Git Tracking

If settings are provided in config.yaml use Go-git package and according to settings perform git commits
This is for both having backups aswell as change management - everything completely offsite.

---

# 7. Config File

`config.yaml`

```yaml
vault:
  path: /data/vault

git:
  autocommit_minutes: 60
  access_token: 3jrkreioöwjfjsdkfoio9dsf
  repository_url: https://github.com/user/repo.git
  commit_message: Automatic SmartSyncServer commit

scan: # this looks for changes in filesystem and writes them to checksums.json
  on_startup: true # perform check when starting server
  api: true # just modify checksums when ever our api changes a file
  on_change:
    false # activate a file watcher that reacts whenever files in the filesystem are changed,
    # which also allows for external filesystem changes made by other programs on the host to be reflected in our checksums
  periodic_interval: 300 # automatically perform file check ever XX minutes

ignore:
  - ".obsidian/workspace*"
  - ".trash/**"
  - "*.tmp"
  - "*.swp"

auth:
  type: token # token | none (for dev)
  token: "your-secret-token"

server:
  port: 8080
  bind: 0.0.0.0
```

## Ignore Patterns

Very important for Obsidian.
You likely want to ignore:

- `.obsidian/workspace*`
- `.obsidian/cache`
- `.DS_Store`
- OS temp files
- Git folder if you store it inside vault

Use **glob patterns**, not regex.  
Much easier to reason about.

---

# 8. Error Handling

Standard JSON error format:
{
"error": "File not found"
}
Use appropriate HTTP status codes:

- 400 — Bad request
- 401 — Unauthorized
- 404 — Not found
- 500 — Server error

---

# 9. Non-Goals

- No server-side conflict resolution
- No multi-user support
- No revision tracking
- No automatic merging
- No HTTPS (handled by reverse proxy)

---

# 10. Implementation Requirements

- Must be stateless (except disk state)
- Must be safe against path traversal
- Must handle concurrent requests safely
- Must be able to run inside Docker
- Must use streaming for single-file GET/PUT where possible
