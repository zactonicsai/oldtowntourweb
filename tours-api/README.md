# Old Town Montgomery Tours — API

A small Go HTTP API for managing self-guided tour sites and their associated audio/video media, plus the HTML admin console that drives it.

## What's here

```
tours-api/
├── cmd/server/          # Entrypoint
├── internal/
│   ├── api/             # HTTP handlers + middleware
│   ├── config/          # Env-var loading
│   ├── models/          # Domain types (Site, MediaObject)
│   └── storage/         # Pluggable persistence (local today, Azure later)
├── web/
│   └── private.html     # Admin console (served by the API if web/ exists)
├── Dockerfile
├── go.mod
└── README.md
```

## Requirements

- Go 1.26 or newer
- Any POSIX-ish host for the data directory (Linux, macOS, WSL)

## Configuration

Every setting is an environment variable. `API_SHARED_KEY` is the only one without a default; the server refuses to start without it.

| Variable          | Default    | Purpose                                                     |
| ----------------- | ---------- | ----------------------------------------------------------- |
| `API_SHARED_KEY`  | *required* | Every `/api/*` request must send `X-API-Key: <this value>`. |
| `PORT`            | `8080`     | HTTP listen port.                                           |
| `DATA_DIR`        | `./data`   | Where `sites.json` and uploaded blobs live.                 |
| `ALLOWED_ORIGINS` | `*`        | Comma-separated CORS allowlist, or `*` for any origin.      |
| `MAX_UPLOAD_MB`   | `100`      | Per-file upload cap.                                        |
| `WEB_ROOT`        | `./web`    | Static HTML dir; unset or empty to disable.                 |

## Running locally

```bash
export API_SHARED_KEY="$(openssl rand -hex 32)"
go mod tidy
go run ./cmd/server
```

Then open `http://localhost:8080/private.html` and enter the same shared key when prompted. The key is cached in `sessionStorage` so you only type it once per browser session.

## API reference

All `/api/*` routes require `X-API-Key`. `/api/media/:id` (GET only) also accepts the key as `?key=...` so `<audio>` and `<video>` tags can stream authenticated bytes.

### Health

```
GET /healthz                  → 200 {"status":"ok"}
```

### Sites

```
GET    /api/sites                    → 200 [Site, ...]
GET    /api/sites/:id                → 200 Site | 404
POST   /api/sites                    → 201 Site            body: SiteInput
PUT    /api/sites/:id                → 200 Site | 404      body: SiteInput
DELETE /api/sites/:id                → 204 | 404
POST   /api/sites/bulk/replace       → 200 [Site, ...]     body: {"sites": [Site, ...]}
POST   /api/sites/bulk/clear         → 204
```

### Media

```
POST   /api/media                    → 201 MediaObject    multipart/form-data, field "file"
                                                          OR raw body (Content-Type set, ?filename=)
GET    /api/media/:id                → 200 <bytes>        streams blob with stored Content-Type
DELETE /api/media/:id                → 204 | 404
GET    /api/media                    → 200 [MediaObject, ...]   metadata only, no bytes
```

A `MediaObject` looks like:

```json
{
  "id": "ff8c6e3a...",
  "url": "/api/media/ff8c6e3a...",
  "filename": "intro.mp3",
  "contentType": "audio/mpeg",
  "size": 1843200,
  "uploadedAt": "2026-04-19T18:30:00Z"
}
```

The `url` is what you store in `Site.audioUrl` / `Site.videoUrl`.

## curl examples

Set these once per shell:

```bash
export KEY="your-shared-key"
export API="http://localhost:8080"
```

Create a site:

```bash
curl -sS -X POST "$API/api/sites" \
  -H "X-API-Key: $KEY" -H "Content-Type: application/json" \
  -d '{"title":"Lucas Tavern","beaconId":"BEACON-001","text":"The oldest building in Montgomery."}'
```

Upload a file:

```bash
curl -sS -X POST "$API/api/media" \
  -H "X-API-Key: $KEY" \
  -F "file=@intro.mp3"
```

Attach the uploaded URL to a site:

```bash
curl -sS -X PUT "$API/api/sites/$SITE_ID" \
  -H "X-API-Key: $KEY" -H "Content-Type: application/json" \
  -d '{"title":"Lucas Tavern","beaconId":"BEACON-001","text":"...","audioUrl":"/api/media/ff8c6e3a..."}'
```

Play it back (no custom headers needed, hence `?key=`):

```bash
curl -sS "$API/api/media/ff8c6e3a...?key=$KEY" -o out.mp3
```

## Data on disk

With `DATA_DIR=./data` the local backend produces:

```
data/
├── sites.json              # whole collection, pretty-printed
└── media/
    ├── <id>.bin            # raw bytes
    └── <id>.json           # metadata (filename, contentType, size, uploadedAt)
```

Writes to `sites.json` are atomic (temp-file-then-rename), so a crash mid-write can't leave corruption behind.

## Future: Azure Blob Storage

The storage layer is deliberately behind two interfaces (`SiteStore`, `MediaStore` in `internal/storage/storage.go`). Swapping in Azure is additive — none of the HTTP handlers change.

Rough plan for the media path:

1. `go get github.com/Azure/azure-sdk-for-go/sdk/storage/azblob`
2. Add `internal/storage/azure_media_store.go` implementing `MediaStore`:
   - `Save`  → `BlockBlobClient.UploadStream`
   - `Open`  → `BlockBlobClient.DownloadStream`
   - `Delete`→ `BlockBlobClient.Delete`
   - `List`  → `ContainerClient.NewListBlobsFlatPager`
3. Add env vars: `AZURE_STORAGE_ACCOUNT`, `AZURE_STORAGE_CONTAINER`, and either `AZURE_STORAGE_KEY` or managed identity via `azidentity.NewDefaultAzureCredential`.
4. In `cmd/server/main.go`, branch on a `MEDIA_BACKEND=local|azure` env var and construct the right store.
5. Decide on URL shape: either return public blob URLs directly (if the container is public) or keep returning `/api/media/:id` and have the handler issue a short-lived SAS redirect. The latter keeps the API key gate in place, which is the simpler migration.

`SiteStore` can stay on local disk indefinitely — it's tiny — or move to Azure Table / Cosmos DB the same way.

## Running with Docker Compose

The included `docker-compose.yml` runs two containers:

- `web` — `nginx:alpine3.23` serving the admin console and reverse-proxying `/api/*` and `/healthz` to the API container. Only this service is published to the host.
- `api` — the Go API. Listens internally on `:8080`, reachable only from the compose network.

Because everything lives behind a single origin, the browser sees just one host/port and CORS stops mattering. The admin console loads from the same origin it talks to for data.

```bash
cp .env.example .env        # fill in API_SHARED_KEY
docker compose up -d --build
open http://localhost:8080/private.html
```

Environment variables the compose file honours (all optional except `API_SHARED_KEY`):

| Var                | Default                  | Effect                                   |
| ------------------ | ------------------------ | ---------------------------------------- |
| `API_SHARED_KEY`   | *required*               | Passed to the API container.             |
| `WEB_PORT`         | `8080`                   | Host port nginx binds to.                |
| `ALLOWED_ORIGINS`  | `*`                      | CORS allowlist on the API.               |
| `MAX_UPLOAD_MB`    | `100`                    | Must not exceed nginx's 100m setting.    |

Data lives in a named volume (`api-data`). Survives `docker compose down`; wiped by `docker compose down -v`.

Tuning upload limits above 100 MB:

1. Bump `MAX_UPLOAD_MB` in `.env`.
2. Edit `deploy/nginx.conf` → `client_max_body_size`.
3. `docker compose up -d` (nginx re-reads the config on restart).

## Running the API standalone in Docker

If you don't need nginx (e.g. you're fronting it with Cloudflare, Azure Front Door, or running locally):

```bash
docker build -t tours-api .
docker run --rm -p 8080:8080 \
  -e API_SHARED_KEY="$(openssl rand -hex 32)" \
  -v tours-data:/data \
  tours-api
```

The image is multi-stage; the final layer is `distroless/static-debian12:nonroot` and runs as uid 65532.

## Tests

```bash
go test ./...
```

The included test covers the `LocalSiteStore` CRUD contract — the component most likely to break silently if the on-disk layout ever changes.
