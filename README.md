# SmartSyncServer

A minimal HTTP API for syncing Obsidian vaults across multiple devices.

## Features

- **SHA256 Checksums** - Matches Obsidian's internal format for efficient sync
- **Bearer Token Auth** - Optional authentication for secure access
- **Git Integration** - Automatic commits to external repository
- **Ignore Patterns** - Glob-style patterns to exclude files
- **Path Safety** - Protection against path traversal attacks
- **Docker Ready** - Containerized deployment

## Quick Start

### Local Development

1. Create `config.yaml` (see [config.yaml](config.yaml))
2. Run the server:
    ```bash
    go run .
    ```
3. Server starts on `http://localhost:8080`

### Docker

```bash
docker-compose up -d
```

## Configuration

See [config.yaml](config.yaml) for all options.

Key settings:

- `vault.path` - Where to store vault files
- `auth.type` - Set to `token` and provide `auth.token` for security
- `scan.on_startup` - Scan vault on server start
- `scan.api` - Update checksums on file changes via API
- `git` - Optional git integration for backups

## API Endpoints

### GET `/status`

Health check (no auth required)

```json
{ "online": true, "file_count": 123 }
```

### GET `/file/{path}`

Download file (raw bytes)

### PUT `/file/{path}`

Upload file (raw bytes)

```json
{ "success": true }
```

### DELETE `/file/{path}`

Delete file

```json
{ "deleted": true }
```

### PUT `/folder/{path}`

Create folder (mkdir -p)

```json
{ "created": true }
```

### DELETE `/folder/{path}`

Delete folder (recursive)

```json
{ "deleted": true }
```

### POST `/rename`

Rename file or folder

```json
{ "type": "file", "from": "notes/a.md", "to": "notes/b.md" }
```

## Authentication

Set in `config.yaml`:

```yaml
auth:
    type: token
    token: your-secret-token
```

Then include header in requests:

```
Authorization: Bearer your-secret-token
```

## Git Integration

Enable git backups in `config.yaml`:

```yaml
git:
    autocommit_minutes: 60 # auto-commit interval (0 = manual only with endpoint)
    access_token: your-github-token
    repository_url: https://github.com/username/repo.git
    commit_message: "Automatic SmartSyncServer commit"
```

The server clones the repo into the vault path on first start, stages all vault
changes, commits, and pushes. Commits run automatically every `autocommit_minutes`
(if > 0) and can also be triggered on demand via the snapshot endpoint.

### Commit Message Placeholders

`commit_message` supports placeholders that are expanded at commit time:

| Placeholder        | Example output     | Meaning                         |
| ------------------ | ------------------ | ------------------------------- |
| `{date_day}`       | `2026-06-20`       | Date                            |
| `{date_daytime}`   | `2026-06-20 14:30` | Date + time (no seconds)        |
| `{date_time}`      | `14:30`            | Clock time (no seconds)         |
| `{date_epoch}`     | `1779000000`       | Unix seconds                    |
| `{files_total}`    | `5`                | Total files in the commit       |
| `{files_added}`    | `2`                | Added (includes renamed/copied) |
| `{files_modified}` | `2`                | Modified                        |
| `{files_deleted}`  | `1`                | Deleted                         |

Examples:

```yaml
  # Commit time as the message:
  commit_message: "{date_time}"

  # Compact change summary:
  commit_message: "{date_time} | +{files_added} ~{files_modified} -{files_deleted}"
```

### Timezone

Time placeholders use the server's **local timezone**. The binary embeds the IANA
timezone database (`time/tzdata`), so it resolves the `TZ` environment variable
even on a bare image without `tzdata` installed.

- **Docker:** set `TZ` in `docker-compose.yml` (e.g. `TZ=Europe/Vienna`).
- **Local:** set the `TZ` env var, or rely on the host's system timezone.

## Requirements Document

See [REQUIREMENTS.md](REQUIREMENTS.md) for full specification.
