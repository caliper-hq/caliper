# Caliper Master Implementation Specification
**Project:** Caliper - Enterprise AI Evaluation Framework
**Paradigm:** Offline-First, Git-Backed, Extensible DAG Architecture
**Languages:** Go (CLI), TypeScript (NestJS + Next.js)

---

## Phase 1: The Core CLI & DAG Engine (v1.0)
**Objective:** Build the foundational offline-first Go CLI that parses a YAML configuration, builds a Directed Acyclic Graph (DAG) of evaluators, and executes them concurrently against a mock provider.

### 1.1 Technical Constraints (Go)
*   **Module Name:** `github.com/your-org/caliper`
*   **CLI Framework:** `github.com/spf13/cobra`
*   **YAML Parser:** `gopkg.in/yaml.v3`
*   **Directory Layout:** Standard Go layout (`/cmd`, `/internal`, `/pkg`).

### 1.2 Agent Tasks
- [ ] **Task 1: Project Scaffolding.** Initialize the Go module and set up the `/cmd/caliper` entry point with a basic `cobra` root command and a sub-command `evaluate`.
- [ ] **Task 2: Define Core Interfaces.** In `/pkg/core/interfaces.go`, define `Provider`, `Evaluator`, `Reporter`, and `EvaluationContext` as strict Go interfaces (do not implement them yet).
- [ ] **Task 3: Define Configuration Structs.** In `/internal/config/config.go`, create the structs to map `caliper.yml` (Config, Profile, DatasetGroup, TestCase).
- [ ] **Task 4: Build the DAG Engine.** In `/internal/engine/dag.go`, implement the concurrent DAG execution loop using Go channels (`readyCh`, `errCh`), `sync.Mutex`, and `sync.WaitGroup`.
- [ ] **Task 5: Implement a Mock Provider & Evaluator.** Create a `MockLLMProvider` that returns a static string, and a `RegexEvaluator` that checks if a string exists in the output.
- [ ] **Task 6: Implement Console Reporter.** Create a `ConsoleReporter` that prints a simple ASCII summary of the DAG execution to standard out.

### 1.3 Acceptance Criteria
*   **UBIQUITOUS:** The CLI shall compile to a single binary with zero external runtime dependencies.
*   **WHEN** the user runs `caliper evaluate --config ./test.yml`, **THE system SHALL** parse the YAML, resolve dependencies, and execute the DAG.
*   **WHEN** an evaluator fails its condition, **THE system SHALL** exit with code `1`.

---

## Phase 2: Local History & Regression Engine (v2.0)
**Objective:** Enable the CLI to detect performance regressions (latency, cost, quality) by comparing the current run against local historical data stored in a `.caliper/` directory.

### 2.1 Technical Constraints
*   **Storage Format:** Sharded JSON (`.caliper/history/YYYY/MM/run-ID.json`).
*   **Diffing Engine:** Use a struct-compare approach before calling out to an AI for semantic diffing.

### 2.2 Agent Tasks
- [ ] **Task 1: Local Storage Adapter.** Create `/internal/storage/local.go`. Implement a function to save `EvaluationResult` as a JSON file in a date-partitioned directory structure.
- [ ] **Task 2: Baseline Retrieval.** Implement a function to retrieve the most recent successful run for a given `DatasetGroup` ID.
- [ ] **Task 3: Regression Evaluator.** Create a new `RegressionEvaluator` node for the DAG. It must calculate Deltas for `LatencyMS`, `CostUSD`, and `OverallScore` against the retrieved baseline.
- [ ] **Task 4: Update YAML Schema.** Add `budget` and `regression` threshold fields to the config structs.

### 2.3 Acceptance Criteria
*   **WHEN** an evaluation completes, **THE system SHALL** write the full telemetry payload to `.caliper/history/`.
*   **WHEN** the calculated latency exceeds the `max_latency_regression` percentage, **THE system SHALL** fail the pipeline run, even if all quality checks pass.

---

## Phase 3: The NestJS Control Plane (v3.0)
**Objective:** Build the opt-in backend API that teams can deploy via Docker to centralize historical runs and aggregate organizational metrics.

### 3.1 Technical Constraints (Node)
*   **Framework:** NestJS (TypeScript).
*   **Database:** PostgreSQL (using TypeORM or Prisma).
*   **Deployment:** Must include a `docker-compose.yml` that stands up the API and DB together.

### 3.2 Agent Tasks
- [ ] **Task 1: NestJS Initialization.** Initialize a new NestJS project in a `/control-plane` directory.
- [ ] **Task 2: Database Schema.** Define the ORM entities for `Project` and `HistoricalRun` matching the Go CLI telemetry payload.
- [ ] **Task 3: Sync Endpoint.** Build `POST /v1/projects/:id/runs`. Ensure it accepts arrays of runs (for bulk syncing).
- [ ] **Task 4: Metrics Endpoint.** Build `GET /v1/projects/:id/metrics` that returns aggregated averages (avg TTFT, avg cost) for the last 30 days.
- [ ] **Task 5: Update Go CLI.** Add a `caliper sync` command to the Go CLI that reads `.caliper/history/`, POSTs it to the NestJS API using a Bearer Token, and marks the local files as `synced: true`.

### 3.3 Acceptance Criteria
*   **UBIQUITOUS:** The NestJS API shall secure all `/v1/` routes requiring a valid Project API key in the Authorization header.
*   **WHEN** a user runs `caliper sync`, **THE system SHALL** successfully transmit local history to the PostgreSQL database without duplicating existing records.

---

## Phase 4: Collaboration Dashboard & Git-Bridge (v4.0)
**Objective:** Build a UI for domain experts to construct test suites visually, and a bridge that converts those visual rules into a Git Pull Request.

### 4.1 Technical Constraints
*   **Frontend:** Next.js (App Router), Tailwind CSS.
*   **Git Integration:** `@octokit/rest` for GitHub API interactions.

### 4.2 Agent Tasks
- [ ] **Task 1: Dashboard UI.** Create a Next.js project in `/dashboard`. Build a visual form where users can add a "Dataset", define a "Prompt", and add "Evaluator Rules" (Regex, Semantic).
- [ ] **Task 2: YAML Serialization.** Write a TypeScript utility that takes the React form state (JSON) and safely serializes it into a valid `caliper.yml` string.
- [ ] **Task 3: The Git-Bridge Endpoint.** In the NestJS API, build `POST /v1/git-bridge/pull-request`. 
- [ ] **Task 4: GitHub API Integration.** Implement the logic in the Git-Bridge endpoint to: branch from `main`, commit the serialized YAML file, and open a Pull Request.

### 4.3 Acceptance Criteria
*   **WHEN** a user clicks "Save Suite" in the UI, **THE system SHALL NOT** save the suite directly to the database.
*   **WHEN** a user clicks "Save Suite", **THE system SHALL** generate a new Pull Request in the target repository containing the updated `caliper.yml` file.