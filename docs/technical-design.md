# Telegram Noise-Reduction Digest Bot Technical Design

## 1) Overview

This system is a modular Telegram aggregator that:
- Uses a **Telegram user account** (MTProto client) to read selected channels.
- Provides a **Telegram bot** for administrative control and configuration.
- **Filters, de-duplicates, clusters, and summarizes** posts using LLMs (OpenAI).
- Publishes a clean, high-quality digest into a **target Telegram channel**.

---

## 2) High-level Requirements

### Functional
1. Channel selection & configuration through a Bot UI:
   - Add/remove/list tracked channels
   - Configure digest cadence (e.g., hourly)
   - Configure filters (keywords allow/deny, language, “ads” patterns, etc.)
2. Ingest channel messages as your user:
   - Capture new messages from tracked channels
   - Persist raw messages
   - **Handle Flood Wait and Rate Limits (human-like behavior)**
3. Pipeline:
   - Deterministic filtering (fast rules)
   - **Semantic deduplication and clustering using pgvector (embeddings)**
   - **LLM-based relevance scoring + batch summarization**
4. Publish:
   - Post digest message to the target channel
   - Include source references
5. Admin/Operations:
   - Health checks
   - Metrics/logging
   - **Leader election for the scheduler**
   - Backpressure controls and retry handling (Dead Letter Office for failed items)

### Non-functional
- Go backend (mono-repo, multi-binary or single binary with roles)
- Postgres with SQL migrations
- Local launch: Docker Compose
- Server launch: Kubernetes (Deployment + CronJob or internal scheduler)
- Secrets handled via Kubernetes Secrets (not ConfigMaps)
- Idempotent digest posting (avoid duplicates on retries)

---

## 3) Architecture

### Components

1. **Control Bot Service (Bot API)**
   - Provides a Telegram UI for administrators to manage tracked channels, filters, and settings.
   - Posts digests to the target channel.
   - Stores configuration in Postgres.

2. **Reader Service (MTProto Client)**
   - Ingests messages from tracked channels as a user.
   - Automatically joins channels via invite links and fetches channel descriptions.
   - Persists raw messages and media (images) to Postgres.

3. **Processor / Pipeline Worker**
   - Applies filters and generates embeddings.
   - Performs semantic deduplication and clustering via `pgvector`.
   - Executes LLM-based relevance scoring and summarization (batched).
   - Features Vision Routing for images and Tiered Importance for critical news.

4. **Digest Scheduler**
   - Implements leader election to ensure only one instance generates digests.
   - Periodically builds digests for configured windows (e.g., 60 minutes).
   - Uses the "Editor-in-Chief" mode to generate cohesive narratives.
   - Marks items as digested to prevent redundancy.

### Data Flow
1. Admin uses bot commands: `/add @channel` → config saved.
2. Reader watches tracked channels → stores raw messages.
3. Processor:
   - Filters and dedups
   - Classifies relevance, assigns topic, creates summary
   - Clusters similar items (optional)
4. Digest scheduler builds a single post per window → bot posts to target channel.

---

## 4) Technology Choices (Go-first)

### Telegram
- **Bot API client (Go):** any mature Go library or direct HTTPS calls.
- **MTProto client (Go):** options include:
  - `gotd/td` (commonly used for MTProto in Go)
  - Alternative: TDLib via a wrapper (more operational complexity)

Recommendation: start with `gotd/td` for the reader.

### LLM
- Use an LLM provider via HTTPS.
- Design for pluggability: `LLMClient` interface with implementations.
- Store prompts and model config per environment.

### Optional: Similarity / clustering
- Start MVP with hash-based dedup (exact/near-exact).
- Add semantic clustering later:
  - Use `pgvector` or separate vector store.
  - Store embeddings in Postgres with `vector` column (requires pgvector extension).

### Infra
- Postgres (with migrations)
- Optional Redis (if you want a queue/backpressure); otherwise DB-driven job tables.

---

## 5) Repository / Service Layout

###  Single binary with “modes”
- `app --mode=bot`
- `app --mode=reader`
- `app --mode=worker`
- `app --mode=digest`

Project structure:
```
/cmd
  /bot
  /reader
  /worker
  /digest
/internal
  /config
  /db
  /telegrambot
  /telegramreader
  /pipeline
  /digest
  /llm
  /dedup
  /filters
  /observability
/migrations
/deploy
  /compose
  /k8s
```

---

## 6) Database Schema (SQL)

### Extensions
- Always:
```sql
CREATE EXTENSION IF NOT EXISTS "uuid-ossp";
```

- Optional (for embeddings):
```sql
CREATE EXTENSION IF NOT EXISTS vector;
```

### Tables

#### 6.1 channels
Stores tracked channels.
```sql
CREATE TABLE IF NOT EXISTS channels (
  id                UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
  tg_peer_id        BIGINT NOT NULL,              -- MTProto peer/channel ID
  username          TEXT,                         -- optional
  title             TEXT,
  is_active         BOOLEAN NOT NULL DEFAULT TRUE,
  added_at          TIMESTAMPTZ NOT NULL DEFAULT now(),
  added_by_tg_user  BIGINT,                        -- admin TG user id (optional)
  access_hash       BIGINT,                        -- required for private channels
  invite_link       TEXT,                          -- stored for joining
  context           TEXT,                          -- manual channel context
  description       TEXT                           -- auto-fetched channel description
);

CREATE UNIQUE INDEX IF NOT EXISTS channels_peer_id_uq ON channels (tg_peer_id);
CREATE INDEX IF NOT EXISTS channels_active_idx ON channels (is_active);
```

#### 6.2 settings
Stores system-wide configuration overrides. Values in this table take precedence over environment variables.
```sql
CREATE TABLE IF NOT EXISTS settings (
  key   TEXT PRIMARY KEY,
  value JSONB NOT NULL,
  updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
```

#### 6.3 raw_messages
Raw ingested messages.
```sql
CREATE TABLE IF NOT EXISTS raw_messages (
  id               UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
  channel_id        UUID NOT NULL REFERENCES channels(id),
  tg_message_id     BIGINT NOT NULL,
  tg_date           TIMESTAMPTZ NOT NULL,
  text              TEXT,
  entities_json     JSONB,
  media_json        JSONB,
  media_data        BYTEA,                  -- downloaded image content
  canonical_hash    TEXT NOT NULL,          -- sha256(canonicalized text)
  is_forward        BOOLEAN NOT NULL DEFAULT FALSE,
  inserted_at       TIMESTAMPTZ NOT NULL DEFAULT now(),
  processed_at      TIMESTAMPTZ
);

-- ensure we don't store duplicates of the same TG message
CREATE UNIQUE INDEX IF NOT EXISTS raw_messages_uq ON raw_messages (channel_id, tg_message_id);
-- Partial index for fast lookup of unprocessed messages
CREATE INDEX IF NOT EXISTS raw_messages_unprocessed_idx ON raw_messages (processed_at) WHERE processed_at IS NULL;
CREATE INDEX IF NOT EXISTS raw_messages_hash_idx ON raw_messages (canonical_hash);
CREATE INDEX IF NOT EXISTS raw_messages_date_idx ON raw_messages (tg_date);
```

#### 6.4 items
Processed messages that survived filtering and got annotated by the pipeline.
`status='error'` acts as the **Dead Letter Office** for processing failures.
```sql
CREATE TABLE IF NOT EXISTS items (
  id                 UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
  raw_message_id      UUID NOT NULL REFERENCES raw_messages(id),
  relevance_score     REAL NOT NULL DEFAULT 0,     -- 0..1
  importance_score    REAL NOT NULL DEFAULT 0,     -- 0..1
  topic              TEXT,
  summary             TEXT,
  language           TEXT,
  status             TEXT NOT NULL DEFAULT 'ready',  -- ready|rejected|error
  error_json          JSONB,
  created_at         TIMESTAMPTZ NOT NULL DEFAULT now(),
  digested_at        TIMESTAMPTZ                   -- when it was included in a digest
);

CREATE UNIQUE INDEX IF NOT EXISTS items_raw_message_uq ON items(raw_message_id);
CREATE INDEX IF NOT EXISTS items_status_idx ON items(status);
CREATE INDEX IF NOT EXISTS items_scores_idx ON items(importance_score DESC, relevance_score DESC);
```

#### 6.5 embeddings
**Required** for semantic deduplication and clustering.
```sql
CREATE TABLE IF NOT EXISTS embeddings (
  item_id     UUID PRIMARY KEY REFERENCES items(id) ON DELETE CASCADE,
  embedding   vector(1536),              -- dimension depends on model
  created_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS embeddings_ivfflat_idx ON embeddings USING ivfflat (embedding vector_cosine_ops);
```

#### 6.6 clusters (optional)
Clusters group semantically similar items within a time window.
```sql
CREATE TABLE IF NOT EXISTS clusters (
  id           UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
  window_start TIMESTAMPTZ NOT NULL,
  window_end   TIMESTAMPTZ NOT NULL,
  topic        TEXT,
  created_at   TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS clusters_window_idx ON clusters (window_start, window_end);
```

#### 6.7 cluster_items (optional)
```sql
CREATE TABLE IF NOT EXISTS cluster_items (
  cluster_id UUID NOT NULL REFERENCES clusters(id) ON DELETE CASCADE,
  item_id    UUID NOT NULL REFERENCES items(id) ON DELETE CASCADE,
  PRIMARY KEY (cluster_id, item_id)
);
```

#### 6.8 digests
Tracks each posted digest window and message id to ensure idempotency.
```sql
CREATE TABLE IF NOT EXISTS digests (
  id             UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
  window_start   TIMESTAMPTZ NOT NULL,
  window_end     TIMESTAMPTZ NOT NULL,
  posted_chat_id BIGINT,                -- target channel id
  posted_msg_id  BIGINT,                -- TG message id created by bot
  status         TEXT NOT NULL DEFAULT 'created', -- created|posted|error
  error_json     JSONB,
  created_at     TIMESTAMPTZ NOT NULL DEFAULT now(),
  posted_at      TIMESTAMPTZ
);

CREATE UNIQUE INDEX IF NOT EXISTS digests_window_uq ON digests (window_start, window_end);
CREATE INDEX IF NOT EXISTS digests_status_idx ON digests (status);
```

#### 6.9 digest_entries
```sql
CREATE TABLE IF NOT EXISTS digest_entries (
  id           UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
  digest_id    UUID NOT NULL REFERENCES digests(id) ON DELETE CASCADE,
  title        TEXT,
  body         TEXT NOT NULL,
  sources_json JSONB NOT NULL,     -- [{channel, msg_id, link, ...}, ...]
  created_at   TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS digest_entries_digest_idx ON digest_entries(digest_id);
```

---

## 7) Bot UX (Commands)

### Core commands
- `/start`, `/help` – show help + current status
- `/settings` – view all current system settings
- `/add <@username|ID|invite_link>` – add tracking target
- `/remove <@username|ID>` – remove target
- `/list` – list tracked channels with their context and description
- `/channelcontext <id> <text>` – set manual context for a channel
- `/target <id or @username>` – set digest destination chat
- `/window <duration>` – digest window (e.g. 60m)
- `/language <lang>` – set digest language (e.g. en, ru)
- `/filters` – list and manage keyword filters
- `/filters add <allow|deny> <pattern>`
- `/filters remove <pattern>`
- `/filters ads <on|off>`
- `/model <name>` – primary LLM model
- `/smartmodel <name>` – flagship LLM model
- `/relevance <0.0-1.0>` – relevance threshold
- `/importance <0.0-1.0>` – importance threshold
- `/topics <on|off>` – topic grouping toggle
- `/dedup <strict|semantic>` – deduplication mode
- `/editor <on|off>` – Editor-in-Chief narrative toggle
- `/tiered <on|off>` – Tiered Importance analysis toggle
- `/visionrouting <on|off>` – Vision-Specific routing toggle
- `/consolidated <on|off>` – Merged similar messages toggle
- `/editordetails <on|off>` – Show details when narrative is enabled
- `/errors` – List recent processing failures
- `/retry` – Requeue failed items

### 11) Observability

The system exports structured metrics in Prometheus format via the `/metrics` endpoint on the configured `HEALTH_PORT` (default 8080).

**Key Metrics:**
- `digest_messages_ingested_total`: Counter of messages fetched from Telegram, labeled by channel.
- `digest_pipeline_processed_total`: Counter of messages processed by the pipeline, labeled by status (ready, rejected, error).
- `digest_llm_request_duration_seconds`: Histogram of LLM API call durations, labeled by model.
- `digest_pipeline_backlog_count`: Gauge of unprocessed messages currently in the queue.
- `digest_posts_total`: Counter of digest messages successfully posted to the target channel, labeled by status (posted, error).

### 12) Graceful Shutdown

All system components (Reader, Worker, Bot, Scheduler) implement graceful shutdown logic. Upon receiving `SIGINT` or `SIGTERM`, they will:
1. Finish processing the current batch or message.
2. Complete any in-flight database or LLM operations.
3. Cleanly exit the main loop and respect the context cancellation.

### Admin guardrails
- Restrict bot admin commands to your Telegram user ID(s).
- Store allowed admin IDs in `settings`.

---

## 8) Processing Pipeline (MVP → Enhanced)

### 8.1 Canonicalization & Embedding (MVP)
Canonicalize text:
- lowercase
- trim whitespace
- remove URLs (or normalize them)
- remove repeated spaces
- optionally remove emoji/punctuation (configurable)

Compute:
- Generate embeddings for `canonical_text` using a service (e.g., OpenAI `text-embedding-3-small`).

Dedup rules (Semantic):
- Compare new embedding against existing embeddings in the last N days using cosine similarity.
- If similarity > threshold (e.g., 0.9) → reject as duplicate.
- If same channel+tg_message_id exists → ignore (DB constraint).

### 8.2 Filtering (MVP)
- deny keywords → reject
- allow keywords (optional “only allow if matched”) → reject otherwise
- language check (cheap heuristic) if needed
- length threshold
- “ad-like” regex patterns (config)

### 8.3 LLM classification + summarization

To optimize costs and performance, the pipeline processes candidate items in **batches**.

For each batch of items:
- **Vision Routing**: If an item contains image data, it is automatically routed to a flagship model (e.g., GPT-4o) with superior visual reasoning.
- **Context Enrichment**: The LLM receives the source channel's title, its description, and a history of recent messages to correctly interpret tone and ongoing narratives.
- **Multi-Score Analysis**: The LLM returns:
  - `relevance_score` (0..1)
  - `importance_score` (0..1)
  - `topic` (string)
  - `summary` (1–2 sentences)
  - `language` (source language detection)
- **Tiered Importance**: Items with very high importance scores (e.g., > 0.8) are automatically re-analyzed by a flagship model to ensure maximum quality and accuracy.

Results are stored in the `items` table. 
- On successful processing, status is set to `ready`.
- Failures are recorded with `status='error'` and details are stored in `error_json` for debugging.

### 8.4 Semantic clustering (Later)
- Generate embeddings for `items` in window
- Cluster by cosine similarity (threshold configurable)
- Create `clusters` and `cluster_items`
- Digest entries become “cluster summaries” with multiple sources

---

## 9) Digest Generation

### Windowing
- Define windows aligned to schedule:
  - Example hourly: `window_end = now() truncated to hour`, `window_start = window_end - 1h`

### Selection logic

- Select top N `items` where:
  - status is `ready`
  - `importance_score` >= `importance_threshold`
  - `digested_at` IS NULL (prevents including same item in multiple digests)
- Items are ordered by `importance_score DESC` and `relevance_score DESC`.

### Formatting

- **Editor-in-Chief Narrative**: If enabled, a flagship model generates a cohesive narrative overview of all items in the window.
- **Topics & Folders**: Related items are grouped by topic. Clusters with multiple items are displayed with a folder emoji (`📂`).
- **Consolidated Clusters**: If enabled, multiple similar messages in a cluster are merged into a single high-quality bullet point with multiple source links.
- **Source Links**: Every item includes HTML links to the original Telegram messages (e.g., `[Source]`).
- **Localization**: Headers and summaries are localized based on the `digest_language` setting.

### Idempotency

- Insert into `digests` with unique `(window_start, window_end)`.
- If a successful post already exists for the window, the run is skipped.
- Failed attempts are recorded with `status='error'` and can be retried after a back-off period.
- Items successfully posted are marked with `digested_at = now()`.

---

## 10) Configuration & Secrets

### Required secrets
- Bot token
- MTProto session credentials (api_id/api_hash + session storage)
- **MTProto 2FA Password** (required if 2FA is enabled on the user account)
- LLM API key
- Postgres DSN

### Environment variables (example)
- `APP_ENV=local|prod`
- `POSTGRES_DSN=...`
- `BOT_TOKEN=...`
- `ADMIN_IDS=123,456`
- `TARGET_CHAT_ID=-100...`
- `TG_API_ID=...`
- `TG_API_HASH=...`
- `TG_2FA_PASSWORD=...`
- `TG_SESSION_PATH=/data/tg.session`
- `LLM_API_KEY=...`
- `DIGEST_WINDOW=60m`
- `DIGEST_TOP_N=20`
- `RELEVANCE_THRESHOLD=0.5`
- `IMPORTANCE_THRESHOLD=0.3`
- `RATE_LIMIT_RPS=...`
- **`LEADER_ELECTION_ENABLED=true`**
- **`LEADER_ELECTION_LEASE_NAME=digest-scheduler-lease`**

---

## 11) Docker Compose (Local)

Example `docker-compose.yml`:
```yaml
services:
  postgres:
    image: postgres:16
    environment:
      POSTGRES_USER: app
      POSTGRES_PASSWORD: app
      POSTGRES_DB: digest
    ports:
      - "5432:5432"
    volumes:
      - pgdata:/var/lib/postgresql/data

  bot:
    build: .
    command: ["./bot"]
    environment:
      APP_ENV: local
      POSTGRES_DSN: postgres://app:app@postgres:5432/digest?sslmode=disable
      BOT_TOKEN: ${BOT_TOKEN}
      ADMIN_IDS: ${ADMIN_IDS}
    depends_on:
      - postgres

  reader:
    build: .
    command: ["./reader"]
    environment:
      APP_ENV: local
      POSTGRES_DSN: postgres://app:app@postgres:5432/digest?sslmode=disable
      TG_API_ID: ${TG_API_ID}
      TG_API_HASH: ${TG_API_HASH}
      TG_SESSION_PATH: /data/tg.session
    volumes:
      - tgdata:/data
    depends_on:
      - postgres

  worker:
    build: .
    command: ["./worker"]
    environment:
      APP_ENV: local
      POSTGRES_DSN: postgres://app:app@postgres:5432/digest?sslmode=disable
      LLM_API_KEY: ${LLM_API_KEY}
    depends_on:
      - postgres

  digest:
    build: .
    command: ["./digest"]
    environment:
      APP_ENV: local
      POSTGRES_DSN: postgres://app:app@postgres:5432/digest?sslmode=disable
      BOT_TOKEN: ${BOT_TOKEN}
      TARGET_CHAT_ID: ${TARGET_CHAT_ID}
      DIGEST_WINDOW: 60m
    depends_on:
      - postgres

volumes:
  pgdata:
  tgdata:
```

Notes:
- `reader` persists the MTProto session under `tgdata`.
- Consider running `digest` as a loop scheduler locally (e.g., every 10 minutes check if window ended), while in Kubernetes you may prefer CronJob.

---

## 12) Kubernetes (Server)

### 12.1 Deployment strategy
- Deploy `bot`, `reader`, `worker` as Deployments.
- Run `digest` as:
  - **CronJob** (recommended for predictable schedules), or
  - Deployment with leader election if you want dynamic cadence.

### 12.2 Example manifests (skeleton)

#### Secret (example)
```yaml
apiVersion: v1
kind: Secret
metadata:
  name: digest-secrets
type: Opaque
stringData:
  POSTGRES_DSN: "postgres://..."
  BOT_TOKEN: "..."
  LLM_API_KEY: "..."
  TG_API_ID: "..."
  TG_API_HASH: "..."
  TARGET_CHAT_ID: "-100..."
```

#### ConfigMap (example)
```yaml
apiVersion: v1
kind: ConfigMap
metadata:
  name: digest-config
data:
  APP_ENV: "prod"
  DIGEST_WINDOW: "60m"
  DIGEST_TOP_N: "20"
  RELEVANCE_THRESHOLD: "0.5"
```

#### Bot Deployment
```yaml
apiVersion: apps/v1
kind: Deployment
metadata:
  name: digest-bot
spec:
  replicas: 1
  selector:
    matchLabels: { app: digest-bot }
  template:
    metadata:
      labels: { app: digest-bot }
    spec:
      containers:
        - name: bot
          image: yourrepo/digest-bot:latest
          envFrom:
            - configMapRef: { name: digest-config }
            - secretRef: { name: digest-secrets }
          ports:
            - containerPort: 8080
```

#### Reader Deployment (with persistent volume for MTProto session)
```yaml
apiVersion: apps/v1
kind: Deployment
metadata:
  name: digest-reader
spec:
  replicas: 1
  selector:
    matchLabels: { app: digest-reader }
  template:
    metadata:
      labels: { app: digest-reader }
    spec:
      containers:
        - name: reader
          image: yourrepo/digest-reader:latest
          envFrom:
            - configMapRef: { name: digest-config }
            - secretRef: { name: digest-secrets }
          volumeMounts:
            - name: tg-session
              mountPath: /data
      volumes:
        - name: tg-session
          persistentVolumeClaim:
            claimName: tg-session-pvc
```

#### Worker Deployment
```yaml
apiVersion: apps/v1
kind: Deployment
metadata:
  name: digest-worker
spec:
  replicas: 1
  selector:
    matchLabels: { app: digest-worker }
  template:
    metadata:
      labels: { app: digest-worker }
    spec:
      containers:
        - name: worker
          image: yourrepo/digest-worker:latest
          envFrom:
            - configMapRef: { name: digest-config }
            - secretRef: { name: digest-secrets }
```

#### Digest CronJob
```yaml
apiVersion: batch/v1
kind: CronJob
metadata:
  name: digest-cron
spec:
  schedule: "0 * * * *"   # hourly
  jobTemplate:
    spec:
      template:
        spec:
          restartPolicy: OnFailure
          containers:
            - name: digest
              image: yourrepo/digest-digest:latest
              envFrom:
                - configMapRef: { name: digest-config }
                - secretRef: { name: digest-secrets }
```

---

## 13) Observability & Operations

### Logs
- Structured JSON logs (zap/zerolog)
- Correlation IDs per pipeline run / digest run

### Metrics
- Ingest rate: messages/min per channel
- Pipeline throughput: processed/min, rejected/min
- LLM latency and error rate
- Digest posting success/failure
- Queue/backlog depth (if DB-driven: count of unprocessed raw_messages)

### Health checks
- `/healthz` (process)
- `/readyz` (DB reachable + dependencies OK)

---

## 14) Security & Guardrails

- Bot admin commands restricted to specific Telegram user IDs.
- Minimal permissions: bot only needs to post to target channel + reply in admin chat.
- **MTProto Reader Guardrails:**
  - Implement **Flood Wait** handling: pause ingestion when Telegram returns a 420 error.
  - **Human-like behavior:** jittered delays between channel polls, avoid high-frequency bursts.
  - **2FA Support:** `reader` must support initial login using API ID, Hash, and 2FA password (if set).
- Store MTProto session securely (PVC + restricted access).
- Encrypt secrets at rest (K8s secret encryption) if available.
- Avoid storing unnecessary personal data. Store only what’s required for digest generation.

---

### 14.1 Initial MTProto Login
To perform the initial login for the Reader service:
1. Set your phone number in the `TG_PHONE` environment variable in your `.env` file.
2. Start the services using Docker Compose: `docker-compose up`.
3. When the Reader service prompts for a verification code, enter it directly in the terminal.
4. If you are running in detached mode (`-d`), you can attach to the reader container to enter the code:
   ```bash
   docker attach $(docker compose ps -q reader)
   ```
5. Once authenticated, a session file will be created in the path specified by `TG_SESSION_PATH` (default `./data/tg.session`), which is persisted via a Docker volume.

---

## 15) Acceptance Criteria

The system is considered operational when:
1. Administrators can manage channels and filters via the bot.
2. The reader successfully ingests messages and media from all tracked channels.
3. The pipeline correctly processes, summarizes, and de-duplicates news.
4. Periodic digests are delivered to the target channel with correct formatting and narrative.
