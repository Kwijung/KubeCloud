# Portal MVP Scaffold Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Create the first runnable Kloudia Portal MVP scaffold with a Go API, React TypeScript frontend, PostgreSQL local dev wiring, and a health check.

**Architecture:** Keep backend and frontend as separate apps under `backend/` and `frontend/`. The backend exposes `/healthz` and `/api/v1/health` first, with config and PostgreSQL connectivity isolated so KubeVirt adapters can be added next. The frontend is a quiet operational UI shell that calls the backend health endpoint and establishes navigation structure for Projects, Images, and VMs.

**Tech Stack:** Go, chi, pgx, React, TypeScript, Vite, TanStack Query, PostgreSQL, Docker Compose, Make.

---

## Scope

This plan builds only the scaffold. It does not implement OIDC, project RBAC, KubeVirt clients, CDI import APIs, VM APIs, or Helm charts. Those come after the scaffold is runnable.

The current workspace is not a Git repository, so commit steps are omitted.

## File Structure

- Create: `backend/go.mod`
  - Go module and backend dependencies.
- Create: `backend/cmd/api/main.go`
  - API server entrypoint.
- Create: `backend/internal/config/config.go`
  - Environment-based configuration.
- Create: `backend/internal/httpapi/router.go`
  - HTTP router and health endpoints.
- Create: `backend/internal/store/postgres.go`
  - PostgreSQL pool creation and ping.
- Create: `backend/internal/httpapi/router_test.go`
  - Health route tests.
- Create: `frontend/package.json`
  - Frontend scripts and dependencies.
- Create: `frontend/index.html`
  - Vite entry HTML.
- Create: `frontend/src/main.tsx`
  - React entrypoint.
- Create: `frontend/src/App.tsx`
  - Operational UI shell.
- Create: `frontend/src/api.ts`
  - Backend health API client.
- Create: `frontend/src/styles.css`
  - App layout styles.
- Create: `docker-compose.yml`
  - Local PostgreSQL.
- Create: `.env.example`
  - Shared local development defaults.
- Create: `Makefile`
  - Common dev/test commands.
- Create: `README.md`
  - Local scaffold run instructions.

## Task 1: Backend Health API

**Files:**
- Create: `backend/go.mod`
- Create: `backend/cmd/api/main.go`
- Create: `backend/internal/config/config.go`
- Create: `backend/internal/httpapi/router.go`
- Create: `backend/internal/httpapi/router_test.go`

- [ ] **Step 1: Write failing router tests**

Create `backend/internal/httpapi/router_test.go`:

```go
package httpapi

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestHealthzReturnsOK(t *testing.T) {
	router := NewRouter("test", nil)
	req := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	rec := httptest.NewRecorder()

	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d", rec.Code)
	}
	var body map[string]string
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if body["status"] != "ok" {
		t.Fatalf("expected status ok, got %q", body["status"])
	}
	if body["service"] != "kloudia-api" {
		t.Fatalf("expected service kloudia-api, got %q", body["service"])
	}
}

func TestAPIHealthIncludesVersion(t *testing.T) {
	router := NewRouter("dev", nil)
	req := httptest.NewRequest(http.MethodGet, "/api/v1/health", nil)
	rec := httptest.NewRecorder()

	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d", rec.Code)
	}
	var body map[string]string
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if body["version"] != "dev" {
		t.Fatalf("expected version dev, got %q", body["version"])
	}
}
```

- [ ] **Step 2: Run tests and verify they fail**

Run:

```bash
cd backend && go test ./...
```

Expected: FAIL because `go.mod` and `NewRouter` do not exist yet.

- [ ] **Step 3: Add Go module**

Create `backend/go.mod`:

```go
module github.com/kloudia/kloudia/backend

go 1.23

require (
	github.com/go-chi/chi/v5 v5.2.2
	github.com/go-chi/cors v1.2.2
)
```

- [ ] **Step 4: Add config package**

Create `backend/internal/config/config.go`:

```go
package config

import (
	"net/url"
	"os"
	"strconv"
)

type Config struct {
	Addr        string
	Version     string
	DatabaseURL string
	CORSOrigin  string
}

func Load() Config {
	return Config{
		Addr:        env("KLOUDIA_API_ADDR", ":8080"),
		Version:     env("KLOUDIA_VERSION", "dev"),
		DatabaseURL: env("DATABASE_URL", "postgres://kloudia:kloudia@localhost:5432/kloudia?sslmode=disable"),
		CORSOrigin:  env("KLOUDIA_CORS_ORIGIN", "http://localhost:5173"),
	}
}

func env(key string, fallback string) string {
	value := os.Getenv(key)
	if value == "" {
		return fallback
	}
	return value
}

func IsLocalhostOrigin(origin string) bool {
	parsed, err := url.Parse(origin)
	if err != nil {
		return false
	}
	host := parsed.Hostname()
	if host == "localhost" || host == "127.0.0.1" {
		return true
	}
	_, err = strconv.Atoi(parsed.Port())
	return err == nil && host != ""
}
```

- [ ] **Step 5: Add router implementation**

Create `backend/internal/httpapi/router.go`:

```go
package httpapi

import (
	"encoding/json"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/cors"
)

type Store interface {
	Ping(r *http.Request) error
}

func NewRouter(version string, store Store) http.Handler {
	router := chi.NewRouter()
	router.Use(cors.Handler(cors.Options{
		AllowedOrigins:   []string{"http://localhost:5173"},
		AllowedMethods:   []string{http.MethodGet, http.MethodPost, http.MethodPut, http.MethodDelete, http.MethodOptions},
		AllowedHeaders:   []string{"Accept", "Authorization", "Content-Type"},
		AllowCredentials: true,
		MaxAge:           300,
	}))

	router.Get("/healthz", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, map[string]string{
			"service": "kloudia-api",
			"status":  "ok",
		})
	})

	router.Get("/api/v1/health", func(w http.ResponseWriter, r *http.Request) {
		status := "ok"
		if store != nil {
			if err := store.Ping(r); err != nil {
				status = "degraded"
			}
		}
		writeJSON(w, http.StatusOK, map[string]string{
			"service": "kloudia-api",
			"status":  status,
			"version": version,
		})
	})

	return router
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}
```

- [ ] **Step 6: Add API entrypoint**

Create `backend/cmd/api/main.go`:

```go
package main

import (
	"log"
	"net/http"

	"github.com/kloudia/kloudia/backend/internal/config"
	"github.com/kloudia/kloudia/backend/internal/httpapi"
)

func main() {
	cfg := config.Load()
	server := &http.Server{
		Addr:    cfg.Addr,
		Handler: httpapi.NewRouter(cfg.Version, nil),
	}

	log.Printf("kloudia api listening on %s", cfg.Addr)
	if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		log.Fatalf("api server failed: %v", err)
	}
}
```

- [ ] **Step 7: Run tests and verify pass**

Run:

```bash
cd backend && go mod tidy && go test ./...
```

Expected: PASS.

## Task 2: PostgreSQL Local Wiring

**Files:**
- Create: `backend/internal/store/postgres.go`
- Modify: `backend/cmd/api/main.go`
- Modify: `backend/go.mod`
- Create: `docker-compose.yml`
- Create: `.env.example`

- [ ] **Step 1: Write failing store test**

Create `backend/internal/store/postgres_test.go`:

```go
package store

import "testing"

func TestNewPostgresRejectsEmptyDatabaseURL(t *testing.T) {
	_, err := NewPostgres("")
	if err == nil {
		t.Fatal("expected error for empty database URL")
	}
}
```

- [ ] **Step 2: Run test and verify failure**

Run:

```bash
cd backend && go test ./...
```

Expected: FAIL because `NewPostgres` does not exist.

- [ ] **Step 3: Add PostgreSQL store**

Create `backend/internal/store/postgres.go`:

```go
package store

import (
	"context"
	"errors"
	"net/http"

	"github.com/jackc/pgx/v5/pgxpool"
)

type Postgres struct {
	pool *pgxpool.Pool
}

func NewPostgres(databaseURL string) (*Postgres, error) {
	if databaseURL == "" {
		return nil, errors.New("database URL is required")
	}
	pool, err := pgxpool.New(context.Background(), databaseURL)
	if err != nil {
		return nil, err
	}
	return &Postgres{pool: pool}, nil
}

func (p *Postgres) Close() {
	if p == nil || p.pool == nil {
		return
	}
	p.pool.Close()
}

func (p *Postgres) Ping(r *http.Request) error {
	if p == nil || p.pool == nil {
		return errors.New("postgres pool is not configured")
	}
	return p.pool.Ping(r.Context())
}
```

- [ ] **Step 4: Wire store into API main**

Modify `backend/cmd/api/main.go` to:

```go
package main

import (
	"log"
	"net/http"

	"github.com/kloudia/kloudia/backend/internal/config"
	"github.com/kloudia/kloudia/backend/internal/httpapi"
	"github.com/kloudia/kloudia/backend/internal/store"
)

func main() {
	cfg := config.Load()
	db, err := store.NewPostgres(cfg.DatabaseURL)
	if err != nil {
		log.Fatalf("database setup failed: %v", err)
	}
	defer db.Close()

	server := &http.Server{
		Addr:    cfg.Addr,
		Handler: httpapi.NewRouter(cfg.Version, db),
	}

	log.Printf("kloudia api listening on %s", cfg.Addr)
	if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		log.Fatalf("api server failed: %v", err)
	}
}
```

- [ ] **Step 5: Add Docker Compose**

Create `docker-compose.yml`:

```yaml
services:
  postgres:
    image: postgres:16
    environment:
      POSTGRES_USER: kloudia
      POSTGRES_PASSWORD: kloudia
      POSTGRES_DB: kloudia
    ports:
      - "5432:5432"
    volumes:
      - kloudia-postgres:/var/lib/postgresql/data
    healthcheck:
      test: ["CMD-SHELL", "pg_isready -U kloudia -d kloudia"]
      interval: 5s
      timeout: 5s
      retries: 10

volumes:
  kloudia-postgres:
```

- [ ] **Step 6: Add environment example**

Create `.env.example`:

```dotenv
KLOUDIA_API_ADDR=:8080
KLOUDIA_VERSION=dev
KLOUDIA_CORS_ORIGIN=http://localhost:5173
DATABASE_URL=postgres://kloudia:kloudia@localhost:5432/kloudia?sslmode=disable
VITE_API_BASE_URL=http://localhost:8080
```

- [ ] **Step 7: Run backend tests**

Run:

```bash
cd backend && go mod tidy && go test ./...
```

Expected: PASS.

## Task 3: Frontend UI Shell

**Files:**
- Create: `frontend/package.json`
- Create: `frontend/index.html`
- Create: `frontend/src/main.tsx`
- Create: `frontend/src/App.tsx`
- Create: `frontend/src/api.ts`
- Create: `frontend/src/styles.css`

- [ ] **Step 1: Add frontend package**

Create `frontend/package.json`:

```json
{
  "name": "kloudia-portal",
  "private": true,
  "version": "0.1.0",
  "type": "module",
  "scripts": {
    "dev": "vite --host 0.0.0.0",
    "build": "tsc -b && vite build",
    "preview": "vite preview --host 0.0.0.0"
  },
  "dependencies": {
    "@tanstack/react-query": "^5.80.10",
    "lucide-react": "^0.468.0",
    "react": "^18.3.1",
    "react-dom": "^18.3.1"
  },
  "devDependencies": {
    "@types/react": "^18.3.18",
    "@types/react-dom": "^18.3.5",
    "@vitejs/plugin-react": "^4.3.4",
    "typescript": "^5.7.3",
    "vite": "^6.0.7"
  }
}
```

- [ ] **Step 2: Add Vite HTML**

Create `frontend/index.html`:

```html
<!doctype html>
<html lang="en">
  <head>
    <meta charset="UTF-8" />
    <meta name="viewport" content="width=device-width, initial-scale=1.0" />
    <title>Kloudia</title>
  </head>
  <body>
    <div id="root"></div>
    <script type="module" src="/src/main.tsx"></script>
  </body>
</html>
```

- [ ] **Step 3: Add API client**

Create `frontend/src/api.ts`:

```ts
export type HealthResponse = {
  service: string;
  status: string;
  version: string;
};

const apiBaseUrl = import.meta.env.VITE_API_BASE_URL ?? "http://localhost:8080";

export async function fetchHealth(): Promise<HealthResponse> {
  const response = await fetch(`${apiBaseUrl}/api/v1/health`);
  if (!response.ok) {
    throw new Error(`health check failed: ${response.status}`);
  }
  return response.json();
}
```

- [ ] **Step 4: Add React entrypoint**

Create `frontend/src/main.tsx`:

```tsx
import React from "react";
import ReactDOM from "react-dom/client";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { App } from "./App";
import "./styles.css";

const queryClient = new QueryClient();

ReactDOM.createRoot(document.getElementById("root")!).render(
  <React.StrictMode>
    <QueryClientProvider client={queryClient}>
      <App />
    </QueryClientProvider>
  </React.StrictMode>,
);
```

- [ ] **Step 5: Add UI shell**

Create `frontend/src/App.tsx`:

```tsx
import { Activity, Boxes, HardDrive, Image, Server } from "lucide-react";
import { useQuery } from "@tanstack/react-query";
import { fetchHealth } from "./api";

const navItems = [
  { label: "Projects", icon: Boxes },
  { label: "Virtual Machines", icon: Server },
  { label: "Images", icon: Image },
  { label: "Storage", icon: HardDrive },
];

export function App() {
  const health = useQuery({
    queryKey: ["health"],
    queryFn: fetchHealth,
    retry: 1,
  });

  return (
    <div className="app-shell">
      <aside className="sidebar">
        <div className="brand">
          <div className="brand-mark">K</div>
          <div>
            <strong>Kloudia</strong>
            <span>Private Cloud</span>
          </div>
        </div>
        <nav>
          {navItems.map((item) => {
            const Icon = item.icon;
            return (
              <button className="nav-item" key={item.label}>
                <Icon size={18} />
                <span>{item.label}</span>
              </button>
            );
          })}
        </nav>
      </aside>

      <main className="workspace">
        <header className="topbar">
          <div>
            <h1>Virtual Machines</h1>
            <p>Project-scoped VM self-service for the first KubeVirt-backed MVP.</p>
          </div>
          <div className="status-pill">
            <Activity size={16} />
            <span>{health.data?.status ?? (health.isLoading ? "checking" : "offline")}</span>
          </div>
        </header>

        <section className="summary-grid">
          <div className="metric">
            <span>Projects</span>
            <strong>0</strong>
          </div>
          <div className="metric">
            <span>VMs</span>
            <strong>0</strong>
          </div>
          <div className="metric">
            <span>Images</span>
            <strong>0</strong>
          </div>
          <div className="metric">
            <span>API</span>
            <strong>{health.data?.version ?? "dev"}</strong>
          </div>
        </section>

        <section className="empty-state">
          <Server size={34} />
          <h2>No virtual machines yet</h2>
          <p>The next implementation slice will add project setup, image import, and VM creation APIs.</p>
          <button>Create VM</button>
        </section>
      </main>
    </div>
  );
}
```

- [ ] **Step 6: Add styles**

Create `frontend/src/styles.css`:

```css
:root {
  font-family: Inter, ui-sans-serif, system-ui, -apple-system, BlinkMacSystemFont, "Segoe UI", sans-serif;
  color: #18212f;
  background: #f5f7fb;
}

* {
  box-sizing: border-box;
}

body {
  margin: 0;
}

button {
  font: inherit;
}

.app-shell {
  min-height: 100vh;
  display: grid;
  grid-template-columns: 260px 1fr;
}

.sidebar {
  background: #111827;
  color: #f8fafc;
  padding: 24px 18px;
}

.brand {
  display: flex;
  gap: 12px;
  align-items: center;
  margin-bottom: 28px;
}

.brand-mark {
  width: 38px;
  height: 38px;
  display: grid;
  place-items: center;
  border-radius: 8px;
  background: #2dd4bf;
  color: #0f172a;
  font-weight: 800;
}

.brand span {
  display: block;
  color: #94a3b8;
  font-size: 13px;
  margin-top: 2px;
}

nav {
  display: grid;
  gap: 6px;
}

.nav-item {
  width: 100%;
  min-height: 40px;
  display: flex;
  align-items: center;
  gap: 10px;
  border: 0;
  border-radius: 6px;
  padding: 9px 10px;
  color: #cbd5e1;
  background: transparent;
  cursor: pointer;
  text-align: left;
}

.nav-item:hover {
  color: #ffffff;
  background: #1f2937;
}

.workspace {
  padding: 28px;
}

.topbar {
  display: flex;
  justify-content: space-between;
  gap: 18px;
  align-items: flex-start;
  margin-bottom: 24px;
}

.topbar h1 {
  margin: 0 0 6px;
  font-size: 28px;
}

.topbar p {
  margin: 0;
  color: #64748b;
}

.status-pill {
  display: inline-flex;
  align-items: center;
  gap: 8px;
  border: 1px solid #cbd5e1;
  border-radius: 999px;
  padding: 7px 12px;
  background: #ffffff;
  color: #334155;
  min-width: 112px;
  justify-content: center;
}

.summary-grid {
  display: grid;
  grid-template-columns: repeat(4, minmax(0, 1fr));
  gap: 14px;
  margin-bottom: 24px;
}

.metric {
  border: 1px solid #d8dee9;
  border-radius: 8px;
  background: #ffffff;
  padding: 16px;
}

.metric span {
  display: block;
  color: #64748b;
  font-size: 13px;
  margin-bottom: 8px;
}

.metric strong {
  font-size: 24px;
}

.empty-state {
  min-height: 360px;
  display: grid;
  place-items: center;
  align-content: center;
  gap: 12px;
  border: 1px dashed #b8c2d2;
  border-radius: 8px;
  background: #ffffff;
  color: #334155;
  text-align: center;
  padding: 28px;
}

.empty-state h2 {
  margin: 0;
  font-size: 20px;
}

.empty-state p {
  margin: 0;
  max-width: 480px;
  color: #64748b;
}

.empty-state button {
  margin-top: 8px;
  border: 0;
  border-radius: 6px;
  padding: 9px 14px;
  background: #2563eb;
  color: #ffffff;
  cursor: pointer;
}

@media (max-width: 760px) {
  .app-shell {
    grid-template-columns: 1fr;
  }

  .sidebar {
    position: static;
  }

  .summary-grid {
    grid-template-columns: repeat(2, minmax(0, 1fr));
  }

  .topbar {
    flex-direction: column;
  }
}
```

- [ ] **Step 7: Build frontend**

Run:

```bash
cd frontend && npm install && npm run build
```

Expected: build succeeds.

## Task 4: Developer Commands and Documentation

**Files:**
- Create: `Makefile`
- Create: `README.md`

- [ ] **Step 1: Add Makefile**

Create `Makefile`:

```makefile
.PHONY: dev-db backend-test frontend-build

dev-db:
	docker compose up -d postgres

backend-test:
	cd backend && go test ./...

frontend-build:
	cd frontend && npm run build
```

- [ ] **Step 2: Add README**

Create `README.md`:

```markdown
# Kloudia

KubeVirt-backed private cloud platform.

## Current Status

This repository contains:

- Infra spike scripts for KubeVirt/CDI validation.
- Initial Portal MVP scaffold.

## Local Development

Start PostgreSQL:

```bash
docker compose up -d postgres
```

Run the backend:

```bash
cd backend
go run ./cmd/api
```

Run the frontend:

```bash
cd frontend
npm install
npm run dev
```

Health checks:

```bash
curl http://localhost:8080/healthz
curl http://localhost:8080/api/v1/health
```

## Infra Spike

See [docs/infra/kubevirt-infra-spike.md](docs/infra/kubevirt-infra-spike.md).
```

- [ ] **Step 3: Verify commands**

Run:

```bash
make backend-test
make frontend-build
```

Expected: both commands pass.

## Task 5: Final Verification

**Files:**
- Verify all scaffold files.

- [ ] **Step 1: Backend test**

Run:

```bash
cd backend && go test ./...
```

Expected: PASS.

- [ ] **Step 2: Frontend build**

Run:

```bash
cd frontend && npm run build
```

Expected: PASS.

- [ ] **Step 3: Start local DB**

Run:

```bash
docker compose up -d postgres
```

Expected: PostgreSQL container healthy or running.

- [ ] **Step 4: Start backend manually**

Run:

```bash
cd backend && go run ./cmd/api
```

Expected: API listens on `:8080`.

- [ ] **Step 5: Check health endpoint**

Run:

```bash
curl http://localhost:8080/healthz
curl http://localhost:8080/api/v1/health
```

Expected: JSON response with `service: kloudia-api`.

