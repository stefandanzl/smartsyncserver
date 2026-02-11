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
{"online": true, "file_count": 123}
```

### GET `/file/{path}`
Download file (raw bytes)

### PUT `/file/{path}`
Upload file (raw bytes)
```json
{"success": true}
```

### DELETE `/file/{path}`
Delete file
```json
{"deleted": true}
```

### PUT `/folder/{path}`
Create folder (mkdir -p)
```json
{"created": true}
```

### DELETE `/folder/{path}`
Delete folder (recursive)
```json
{"deleted": true}
```

### POST `/rename`
Rename file or folder
```json
{"type": "file", "from": "notes/a.md", "to": "notes/b.md"}
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

## Requirements Document

See [REQUIREMENTS.md](REQUIREMENTS.md) for full specification.
