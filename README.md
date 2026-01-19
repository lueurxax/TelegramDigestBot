# Telegram Noise-Reduction Digest Bot

[![codecov](https://codecov.io/github/lueurxax/TelegramDigestBot/graph/badge.svg?token=ZL3DVDXYB6)](https://codecov.io/github/lueurxax/TelegramDigestBot)

A modular Telegram automation system written in Go that ingests messages from tracked channels using an MTProto client (user account), filters them, and generates a summarized digest in a target channel using an LLM (OpenAI).

## Features
- **MTProto Reader**: Ingests messages as a real user, handling Flood Waits and rate limits. Supports public channels, private channels (via invite links), and automatically fetches channel descriptions.
- **Advanced Processing Pipeline**:
    - **Vision Support**: Automatically analyzes images using GPT-4o vision capabilities.
    - **Context Awareness**: Incorporates channel descriptions and previous message history for better understanding.
    - **Multi-Model Routing**: Uses cost-effective models for bulk processing and flagship models (like GPT-4o) for high-importance items or vision tasks.
    - **Semantic Deduplication**: Uses `pgvector` embeddings to identify and skip redundant information across channels.
- **High-Quality Digests**:
    - **Editor-in-Chief Mode**: Generates a cohesive narrative overview of the news instead of just bullet points.
    - **Consolidated Clusters**: Merges similar stories into a single bullet point with multiple source links.
    - **Topic Grouping**: Automatically identifies and groups news by topic.
- **Observability**: 
    - **Prometheus Metrics**: Exports ingest rates, pipeline processing status, LLM latency, and backlog counts via a `/metrics` endpoint.
    - **Graceful Shutdown**: All services support clean termination, ensuring in-flight operations are completed.
- **Control Bot**: Robust Telegram UI for administrators to manage tracked channels, filters, and all system settings. Includes error recovery tools (`/errors`, `/retry`).
- **Digest Scheduler**: Leader-elected scheduler for reliable delivery even in multi-instance deployments.

## Architecture Overview

The system consists of four main components interacting through a shared PostgreSQL database:

1.  **Control Bot**: An administrative interface for managing tracked channels, filters, and all system settings.
2.  **Reader Service**: An MTProto client that ingests messages and media from tracked Telegram channels.
3.  **Processor Worker**: A pipeline that filters messages, generates embeddings, and uses LLMs for relevance scoring and summarization.
4.  **Digest Scheduler**: A leader-elected service that builds and posts digests for specific time windows.

For more detailed technical information, see the [Technical Design](docs/technical-design.md).

## Documentation

Feature documentation is available in `docs/features/`:

- [Content Quality](docs/features/content-quality.md) - Relevance gates, feedback loops, clustering, topic balance
- [Digest Schedule](docs/features/digest-schedule.md) - Timezone-aware scheduling
- [Channel Importance](docs/features/channel-importance-weight.md) - Per-channel importance weighting
- [Channel Discovery](docs/features/discovery.md) - Automatic channel discovery, keyword filters, admin review
- [Editor Mode](docs/features/editor-mode.md) - Narrative rendering, tiered importance, consolidated clusters
- [Vision & Images](docs/features/vision-images.md) - Vision routing, cover images, AI covers
- [Annotations](docs/features/annotations.md) - Item labeling for quality evaluation and threshold tuning
- [Corroboration](docs/features/corroboration.md) - Channel corroboration and fact-check links
- [Link Enrichment](docs/features/link-enrichment.md) - URL resolution and content extraction for better summaries

## Fact Check (Phase 1)

This project can optionally query the Google Fact Check Tools API to attach a “Related fact-check” link to digest items.

### Enable and Configure
1. Create a Google Cloud project and enable **Fact Check Tools API**.
2. Create an API key and (recommended) restrict it by IP or referrer.
3. Set the following in your `.env` or Kubernetes config:
   - `FACTCHECK_GOOGLE_ENABLED=true`
   - `FACTCHECK_GOOGLE_API_KEY=...`
   - `FACTCHECK_GOOGLE_RPM=60` (match your quota)
   - `FACTCHECK_GOOGLE_MAX_RESULTS=3`
   - `FACTCHECK_CACHE_TTL_HOURS=48`
   - `FACTCHECK_MIN_CLAIM_LENGTH=40`
   - `FACTCHECK_QUEUE_MAX=5000` (backpressure limit)

### Quotas and Limits
Check your quota in Google Cloud Console:
**APIs & Services → Fact Check Tools API → Quotas**.  
Set `FACTCHECK_GOOGLE_RPM` to stay below that limit.

## Prerequisites
- Docker and Docker Compose
- Telegram API Credentials (`api_id` and `api_hash` from [my.telegram.org](https://my.telegram.org))
- Telegram Bot Token (from [@BotFather](https://t.me/BotFather))
- OpenAI API Key

## Setup

1. **Clone the repository**:
   ```bash
   git clone https://github.com/lueurxax/telegram-digest-bot.git
   cd telegram-digest-bot
   ```

2. **Configure environment variables**:
   ```bash
   cp .env.example .env
   ```
   Edit `.env` and fill in your credentials.

## Usage (Docker Compose)

### 1. Start the services
```bash
docker-compose -f deploy/compose/docker-compose.yml up -d
```

### 2. Initial MTProto Login (Interactive)
The first time you run the reader, you need to authenticate with your phone number and the verification code sent by Telegram.
1. Ensure `TG_PHONE` is set in your `.env`. It should be in international format starting with `+` (e.g., `+1234567890`).
2. Attach to the reader container:
   ```bash
   docker attach $(docker-compose -f deploy/compose/docker-compose.yml ps -q reader)
   ```
3. Enter the code when prompted. If you have 2FA enabled, you will also be prompted for your password (or it will use `TG_2FA_PASSWORD` from `.env`).
4. Once authenticated, the session is saved to `./data/tg.session`.

### 3. Manage via Control Bot
Message your bot on Telegram and use the following commands:
- `/add <username|ID|invite_link>` - Add a channel to track.
- `/list` - List active tracked channels with their context.
- `/remove <username|ID>` - Stop tracking a channel.
- `/settings` - View all current system configurations.
- `/help` - See a comprehensive list of all commands and features.

The bot supports many advanced features like `/editor`, `/visionrouting`, and `/consolidated` to customize your digest quality and format.

## Maintenance

### Rebuilding the Docker Image
If you have made changes to the code and want to rebuild the images:
```bash
docker-compose -f deploy/compose/docker-compose.yml build
```
To rebuild and restart specific services (e.g., the worker):
```bash
docker-compose -f deploy/compose/docker-compose.yml up -d --build worker
```
To rebuild everything without using the cache:
```bash
docker-compose -f deploy/compose/docker-compose.yml build --no-cache
```

## Development

### Running locally
You can run the components individually using the `--mode` flag:
```bash
go run ./cmd/digest-bot/main.go --mode=bot
go run ./cmd/digest-bot/main.go --mode=reader
go run ./cmd/digest-bot/main.go --mode=worker
go run ./cmd/digest-bot/main.go --mode=digest
```

## Deployment (Kubernetes)
Kubernetes manifests are located in `deploy/k8s/`.
1. Update `secrets.yaml` and `configmap.yaml`.
2. Apply the manifests:
   ```bash
   kubectl apply -f deploy/k8s/
   ```
