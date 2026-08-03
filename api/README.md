# `caliper-api`

[![GitHub License](https://img.shields.io/badge/license-MIT-blue.svg)](LICENSE)
[![NestJS](https://img.shields.io/badge/Framework-NestJS%2011-red.svg)](https://nestjs.com)
[![PostgreSQL](https://img.shields.io/badge/Database-PostgreSQL%2016-blue.svg)](https://www.postgresql.org)
[![Org](https://img.shields.io/badge/Org-caliper--hq-orange.svg)](https://github.com/caliper-hq)

The central control plane API and Git bridge service for **[Caliper](https://github.com/caliper-hq/caliper)**. `caliper-api` handles benchmark run persistence, 30-day metric aggregations, multi-tenant project authentication, and Git Pull Request automation for declarative `caliper.yml` suites.

---

## 🌟 Key Features

- **📊 Run Persistence & Metrics**: Ingests CLI evaluation benchmark runs, calculates average Time-to-First-Token (TTFT), cost, and pass rates over PostgreSQL.
- **🔀 Git PR Automation**: Receives suite configurations from [`caliper-dashboard`](https://github.com/caliper-hq/caliper-dashboard), dynamically creates branches, writes `caliper.yml`, and opens Pull Requests via GitHub API (`@octokit/rest`).
- **🔑 Project Key Authentication**: Multi-tenant authorization using Bearer token verification per project.
- **🔄 Idempotent Ingestion**: Uniquely indexes `(project_id, run_id)` so duplicate sync requests from CI/CD pipelines are safely ignored.

---

## 🚀 API Endpoint Overview

| Method | Endpoint | Description | Auth Required |
| :--- | :--- | :--- | :--- |
| `POST` | `/v1/projects/:id/runs` | Ingest benchmark run records (`{ "runs": [...] }`) | `Bearer <project API key>` |
| `GET` | `/v1/projects/:id/metrics` | Fetch aggregated 30-day metrics (`avg_ttft_ms`, `avg_cost_usd`, pass rate) | `Bearer <project API key>` |
| `POST` | `/v1/git-bridge/pull-request` | Write `caliper.yml` to target workspace repository & open GitHub PR | `Bearer <project API key>` |

---

## 🛠️ Quickstart

### Running via Docker Compose

From the repository root (or using the multi-repo Docker setup):

```bash
BOOTSTRAP_PROJECT_ID=team-a BOOTSTRAP_PROJECT_API_KEY=your-secret-key docker compose up --build
```

### Syncing with the CLI

Point the [`caliper`](https://github.com/caliper-hq/caliper) CLI to `caliper-api`:

```bash
caliper sync --url http://localhost:3000 --project-id team-a
```

Set the API token in environment:
```bash
export CALIPER_API_TOKEN=your-secret-key
```

---

## 🔗 Related Repositories

- **[`caliper-hq/caliper`](https://github.com/caliper-hq/caliper)**: High-performance Go CLI benchmark runner.
- **[`caliper-hq/caliper-dashboard`](https://github.com/caliper-hq/caliper-dashboard)**: Next.js visual suite editor.
