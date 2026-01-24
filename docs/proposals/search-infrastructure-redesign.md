# Search Infrastructure Redesign: YaCy Replacement

> **Status: Proposal** (January 2026)
>
> This proposal addresses the operational challenges with YaCy and proposes a more Kubernetes-native architecture using separate crawler and search index components.

## Summary

Replace the monolithic YaCy deployment with a modular architecture:
1. **Apache SolrCloud** - 3-node search cluster with ZooKeeper for HA (no single point of failure)
2. **News Crawler** - Go Deployment with shared Solr work queue for true load balancing across replicas
3. **Search Adapter** - Thin wrapper implementing the existing `Provider` interface
4. **Telegram Index** *(future)* - Dual-write from Reader for cross-channel verification

## Problem Statement

### Current Issues with YaCy

| Issue | Impact | Frequency |
|-------|--------|-----------|
| **Memory exhaustion** | OOMKilled, pod evictions | Daily (43+ crashes in one week) |
| **Resource requirements** | Requires 6-12GB RAM, dedicated 16GB node | Permanent |
| **Empty index after restart** | Zero search results, enrichment fails | Every restart |
| **Slow index building** | Days to rebuild useful index | Every restart |
| **Monolithic design** | Cannot scale components independently | Architectural |
| **P2P overhead** | DHT networking despite being disabled | Permanent |

### Observed Behavior (January 2026)

```
# Typical YaCy crash cycle
1. Pod starts, requests 6Gi memory
2. Java heap grows during crawling/indexing
3. Memory exceeds limit (8-12Gi)
4. OOMKilled (Exit Code 137)
5. Pod restarts, index lost
6. Repeat every 2-6 hours
```

### Cost Analysis

| Resource | YaCy (Current) | Proposed Architecture |
|----------|----------------|----------------------|
| Memory | 6-12GB dedicated | 2-4GB (Solr) + 256MB (Crawler) |
| CPU | 250m-2000m | 500m (Solr) + 100m (Crawler) |
| Storage | 10GB PVC (often corrupted) | 20GB PVC (Solr, stable) |
| Dedicated node | Required (16GB) | Not required |

## Proposed Architecture

### Component Overview

```
┌─────────────────────────────────────────────────────────────────┐
│                     Kubernetes Cluster                          │
├─────────────────────────────────────────────────────────────────┤
│                                                                 │
│  ┌──────────────────────────────────────┐                      │
│  │   Crawler Deployment (N replicas)    │                      │
│  │  ┌─────────┐ ┌─────────┐ ┌─────────┐ │                      │
│  │  │ crawler │ │ crawler │ │ crawler │ │                      │
│  │  │  (any)  │ │  (any)  │ │  (any)  │ │                      │
│  │  └────┬────┘ └────┬────┘ └────┬────┘ │                      │
│  └───────┼───────────┼───────────┼──────┘                      │
│          │           │           │                              │
│          └───────────┴───────────┘                              │
│                      │ claim/release                            │
│                      ▼                                          │
│  ┌──────────────┐    ┌──────────────┐    ┌──────────────────┐  │
│  │ Seed Config  │    │    Solr      │◀───│  Search Adapter  │  │
│  │ (ConfigMap)  │    │ (StatefulSet)│    │   (in Worker)    │  │
│  └──────────────┘    │  + URL Queue │    └──────────────────┘  │
│                      └──────────────┘                           │
│                             │                     ▲             │
│                             ▼                     │             │
│                      ┌──────────────┐    ┌──────────────────┐  │
│                      │  Solr PVC    │    │ Enrichment Worker│  │
│                      │ (Persistent) │    │                  │  │
│                      └──────────────┘    └──────────────────┘  │
│                                                                 │
└─────────────────────────────────────────────────────────────────┘
```

### Design Decisions

| Decision | Choice | Rationale |
|----------|--------|-----------|
| **Solr mode** | SolrCloud with 3 replicas + ZooKeeper | High availability, no single point of failure |
| **Unique document key** | SHA256 hash of canonical URL (web: `https://...`, telegram: `tg://peer/<id>/msg/<id>`) | Deterministic, handles URL variations |
| **Multi-language** | Single collection with `language` field + dynamic fields | Correct stemming per language, filtering via `fq` |
| **Language indexing** | Dynamic fields (`title_en`, `content_ru`, etc.) at index time | Avoids wrong-stemmer analysis from copyFields |
| **Language detection timing** | Web: at crawl time; Telegram: "unknown" at ingest, updated by worker | Reader is fast path, detection is expensive |
| **Stopwords** | Per-language stopword filters (en, ru, el) using Solr built-in lists | Improves relevance by ignoring common words |
| **Crawler type** | Deployment (stateless) | No ordinals needed; work queue provides load balancing |
| **Work distribution** | Shared URL queue in Solr | True load balancing; large domains spread across all crawlers |
| **Queue storage** | Solr `crawl_status` field | No extra infrastructure |
| **Claim atomicity** | Solr `_version_` optimistic locking | 409 Conflict on concurrent claim attempts |
| **URL discovery** | RSS/Sitemap first, fallback to link crawling | Efficient, respects site structure |
| **Content extraction** | go-readability (Mozilla algorithm) | Clean article text, no boilerplate |
| **Index maintenance** | Daily cleanup CronJob | Purge stale pending, old content |

### Component 1: Apache SolrCloud (Search Cluster)

**Purpose**: Store and query indexed news content with high availability

**K8s Resources**:
- ZooKeeper StatefulSet (3 replicas) - cluster coordination
- Solr StatefulSet (3 replicas) - search nodes
- Each Solr node has its own PVC for data persistence

```yaml
apiVersion: v1
kind: ConfigMap
metadata:
  name: solr-config
  namespace: digest
data:
  schema.xml: |
    <?xml version="1.0" encoding="UTF-8"?>
    <schema name="news" version="1.6">
      <!-- Document ID: SHA256 hash of canonical URL -->
      <uniqueKey>id</uniqueKey>

      <field name="id" type="string" indexed="true" stored="true" required="true"/>
      <field name="source" type="string" indexed="true" stored="true" default="web"/>
      <field name="url" type="string" indexed="true" stored="true"/>
      <field name="url_canonical" type="string" indexed="true" stored="true"/>
      <field name="domain" type="string" indexed="true" stored="true"/>
      <field name="language" type="string" indexed="true" stored="true" default="unknown"/>
      <field name="published_at" type="pdate" indexed="true" stored="true"/>
      <field name="crawled_at" type="pdate" indexed="true" stored="true"/>

      <!-- Crawl queue fields (unified: URL queue + indexed content) -->
      <field name="crawl_status" type="string" indexed="true" stored="true" default="pending"/>
      <field name="crawl_claimed_at" type="pdate" indexed="true" stored="true"/>
      <field name="crawl_claimed_by" type="string" indexed="true" stored="true"/>
      <field name="crawl_depth" type="pint" indexed="true" stored="true" default="0"/>
      <field name="crawl_error" type="string" indexed="true" stored="true"/>

      <!-- Text fields with general analyzer (stored for display) -->
      <field name="title" type="text_general" indexed="true" stored="true"/>
      <field name="description" type="text_general" indexed="true" stored="true"/>
      <field name="content" type="text_general" indexed="true" stored="true"/>

      <!-- Combined search field -->
      <field name="text" type="text_general" indexed="true" stored="false" multiValued="true"/>
      <copyField source="title" dest="text"/>
      <copyField source="content" dest="text"/>
      <copyField source="description" dest="text"/>

      <!-- Dynamic fields for language-specific indexing (populated by crawler) -->
      <dynamicField name="*_en" type="text_en" indexed="true" stored="false"/>
      <dynamicField name="*_ru" type="text_ru" indexed="true" stored="false"/>
      <dynamicField name="*_el" type="text_el" indexed="true" stored="false"/>

      <!-- Field types -->
      <fieldType name="string" class="solr.StrField" sortMissingLast="true"/>
      <fieldType name="pint" class="solr.IntPointField" docValues="true"/>
      <fieldType name="plong" class="solr.LongPointField" docValues="true"/>
      <fieldType name="pdate" class="solr.DatePointField" docValues="true"/>
      <fieldType name="text_general" class="solr.TextField" positionIncrementGap="100">
        <analyzer>
          <tokenizer class="solr.StandardTokenizerFactory"/>
          <filter class="solr.LowerCaseFilterFactory"/>
        </analyzer>
      </fieldType>
      <fieldType name="text_en" class="solr.TextField" positionIncrementGap="100">
        <analyzer>
          <tokenizer class="solr.StandardTokenizerFactory"/>
          <filter class="solr.LowerCaseFilterFactory"/>
          <filter class="solr.StopFilterFactory" words="lang/stopwords_en.txt" ignoreCase="true"/>
          <filter class="solr.EnglishPossessiveFilterFactory"/>
          <filter class="solr.PorterStemFilterFactory"/>
        </analyzer>
      </fieldType>
      <fieldType name="text_ru" class="solr.TextField" positionIncrementGap="100">
        <analyzer>
          <tokenizer class="solr.StandardTokenizerFactory"/>
          <filter class="solr.LowerCaseFilterFactory"/>
          <filter class="solr.StopFilterFactory" words="lang/stopwords_ru.txt" ignoreCase="true"/>
          <filter class="solr.SnowballPorterFilterFactory" language="Russian"/>
        </analyzer>
      </fieldType>
      <fieldType name="text_el" class="solr.TextField" positionIncrementGap="100">
        <analyzer>
          <tokenizer class="solr.StandardTokenizerFactory"/>
          <filter class="solr.LowerCaseFilterFactory"/>
          <filter class="solr.StopFilterFactory" words="lang/stopwords_el.txt" ignoreCase="true"/>
          <filter class="solr.GreekStemFilterFactory"/>
        </analyzer>
      </fieldType>
    </schema>

  solrconfig.xml: |
    <?xml version="1.0" encoding="UTF-8"?>
    <config>
      <luceneMatchVersion>9.4</luceneMatchVersion>
      <dataDir>${solr.data.dir:}</dataDir>
      <directoryFactory name="DirectoryFactory" class="${solr.directoryFactory:solr.NRTCachingDirectoryFactory}"/>
      <schemaFactory class="ClassicIndexSchemaFactory"/>
      <requestHandler name="/select" class="solr.SearchHandler">
        <lst name="defaults">
          <str name="echoParams">explicit</str>
          <str name="wt">json</str>
          <str name="indent">true</str>
          <str name="df">text</str>
          <str name="defType">edismax</str>
          <str name="qf">title^2.0 description^1.5 content^1.0</str>
        </lst>
      </requestHandler>
      <requestHandler name="/update" class="solr.UpdateRequestHandler"/>
      <requestHandler name="/admin/ping" class="solr.PingRequestHandler">
        <lst name="invariants">
          <str name="q">*:*</str>
        </lst>
      </requestHandler>
    </config>
---
# ZooKeeper StatefulSet (3 nodes for quorum)
apiVersion: apps/v1
kind: StatefulSet
metadata:
  name: zookeeper
  namespace: digest
spec:
  replicas: 3
  serviceName: zookeeper-headless
  selector:
    matchLabels:
      app: zookeeper
  template:
    metadata:
      labels:
        app: zookeeper
    spec:
      containers:
      - name: zookeeper
        image: zookeeper:3.9
        ports:
        - containerPort: 2181
          name: client
        - containerPort: 2888
          name: server
        - containerPort: 3888
          name: election
        env:
        - name: ZOO_MY_ID
          valueFrom:
            fieldRef:
              fieldPath: metadata.name
        - name: ZOO_SERVERS
          value: "server.1=zookeeper-0.zookeeper-headless:2888:3888;2181 server.2=zookeeper-1.zookeeper-headless:2888:3888;2181 server.3=zookeeper-2.zookeeper-headless:2888:3888;2181"
        - name: ZOO_4LW_COMMANDS_WHITELIST
          value: "mntr,conf,ruok"
        command:
        - /bin/bash
        - -c
        - |
          # Extract ordinal from hostname (zookeeper-0 -> 1, zookeeper-1 -> 2, etc.)
          ORDINAL=${HOSTNAME##*-}
          export ZOO_MY_ID=$((ORDINAL + 1))
          exec /docker-entrypoint.sh zkServer.sh start-foreground
        resources:
          requests:
            memory: "512Mi"
            cpu: "100m"
          limits:
            memory: "1Gi"
            cpu: "500m"
        volumeMounts:
        - name: zk-data
          mountPath: /data
        livenessProbe:
          exec:
            command: ["sh", "-c", "echo ruok | nc localhost 2181 | grep imok"]
          initialDelaySeconds: 30
          periodSeconds: 10
        readinessProbe:
          exec:
            command: ["sh", "-c", "echo ruok | nc localhost 2181 | grep imok"]
          initialDelaySeconds: 10
          periodSeconds: 5
  volumeClaimTemplates:
  - metadata:
      name: zk-data
    spec:
      accessModes: ["ReadWriteOnce"]
      resources:
        requests:
          storage: 5Gi
---
apiVersion: v1
kind: Service
metadata:
  name: zookeeper-headless
  namespace: digest
spec:
  clusterIP: None
  selector:
    app: zookeeper
  ports:
  - name: client
    port: 2181
  - name: server
    port: 2888
  - name: election
    port: 3888
---
apiVersion: v1
kind: Service
metadata:
  name: zookeeper
  namespace: digest
spec:
  selector:
    app: zookeeper
  ports:
  - name: client
    port: 2181
---
# SolrCloud StatefulSet (3 nodes)
apiVersion: apps/v1
kind: StatefulSet
metadata:
  name: solr
  namespace: digest
spec:
  replicas: 3
  serviceName: solr-headless
  podManagementPolicy: Parallel
  selector:
    matchLabels:
      app: solr
  template:
    metadata:
      labels:
        app: solr
    spec:
      initContainers:
      # Wait for ZooKeeper to be ready
      - name: wait-for-zk
        image: busybox:1.36
        command: ["sh", "-c", "until nc -z zookeeper 2181; do echo waiting for zookeeper; sleep 2; done"]
      # Upload configset to ZooKeeper (only from solr-0)
      - name: upload-config
        image: solr:9.4
        command:
        - /bin/bash
        - -c
        - |
          set -e

          # Only upload config from first node
          if [ "$HOSTNAME" != "solr-0" ]; then
            echo "Skipping config upload on $HOSTNAME"
            exit 0
          fi

          echo "Uploading configset to ZooKeeper..."

          # Create temp configset directory with lang/ subdirectory
          mkdir -p /tmp/newsconf/lang
          cp /config/schema.xml /tmp/newsconf/
          cp /config/solrconfig.xml /tmp/newsconf/

          # Copy stopword files from Solr's built-in language resources
          # These are referenced in schema.xml text_en/text_ru/text_el field types
          cp /opt/solr/server/solr/configsets/_default/conf/lang/stopwords_en.txt /tmp/newsconf/lang/
          cp /opt/solr/server/solr/configsets/_default/conf/lang/stopwords_ru.txt /tmp/newsconf/lang/ 2>/dev/null || \
            curl -sL "https://raw.githubusercontent.com/apache/lucene/main/lucene/analysis/common/src/resources/org/apache/lucene/analysis/ru/stopwords.txt" > /tmp/newsconf/lang/stopwords_ru.txt
          # Greek stopwords (not in default Solr, fetch from Lucene)
          curl -sL "https://raw.githubusercontent.com/apache/lucene/main/lucene/analysis/common/src/resources/org/apache/lucene/analysis/el/stopwords.txt" > /tmp/newsconf/lang/stopwords_el.txt

          # Upload configset
          /opt/solr/bin/solr zk upconfig -n news -d /tmp/newsconf -z zookeeper:2181

          echo "Configset uploaded"
        volumeMounts:
        - name: solr-config
          mountPath: /config
        resources:
          requests:
            memory: "256Mi"
            cpu: "100m"
      containers:
      - name: solr
        image: solr:9.4
        ports:
        - containerPort: 8983
          name: http
        env:
        - name: SOLR_HOST
          valueFrom:
            fieldRef:
              fieldPath: metadata.name
        - name: ZK_HOST
          value: "zookeeper-0.zookeeper-headless:2181,zookeeper-1.zookeeper-headless:2181,zookeeper-2.zookeeper-headless:2181"
        - name: SOLR_JAVA_MEM
          value: "-Xms1g -Xmx2g"
        - name: GC_TUNE
          value: "-XX:+UseG1GC -XX:MaxGCPauseMillis=200"
        command:
        - /bin/bash
        - -c
        - |
          # Start Solr in cloud mode
          exec solr-foreground -c -z ${ZK_HOST}
        resources:
          requests:
            memory: "2Gi"
            cpu: "250m"
          limits:
            memory: "4Gi"
            cpu: "1000m"
        volumeMounts:
        - name: solr-data
          mountPath: /var/solr
        livenessProbe:
          httpGet:
            path: /solr/admin/info/system
            port: 8983
          initialDelaySeconds: 60
          periodSeconds: 30
          timeoutSeconds: 10
        readinessProbe:
          httpGet:
            path: /solr/admin/info/system
            port: 8983
          initialDelaySeconds: 30
          periodSeconds: 10
          timeoutSeconds: 5
      volumes:
      - name: solr-config
        configMap:
          name: solr-config
  volumeClaimTemplates:
  - metadata:
      name: solr-data
    spec:
      accessModes: ["ReadWriteOnce"]
      resources:
        requests:
          storage: 20Gi
---
apiVersion: v1
kind: Service
metadata:
  name: solr-headless
  namespace: digest
spec:
  clusterIP: None
  selector:
    app: solr
  ports:
  - name: http
    port: 8983
---
apiVersion: v1
kind: Service
metadata:
  name: solr
  namespace: digest
spec:
  selector:
    app: solr
  ports:
  - name: http
    port: 8983
    targetPort: 8983
---
# Job to create collection after cluster is up
apiVersion: batch/v1
kind: Job
metadata:
  name: solr-create-collection
  namespace: digest
spec:
  ttlSecondsAfterFinished: 300
  template:
    spec:
      restartPolicy: OnFailure
      initContainers:
      - name: wait-for-solr
        image: curlimages/curl:8.6.0
        command:
        - /bin/sh
        - -c
        - |
          echo "Waiting for SolrCloud cluster..."
          until curl -s "http://solr:8983/solr/admin/collections?action=CLUSTERSTATUS" | grep -q '"live_nodes"'; do
            sleep 5
          done
          echo "SolrCloud is ready"
      containers:
      - name: create-collection
        image: curlimages/curl:8.6.0
        command:
        - /bin/sh
        - -c
        - |
          # Check if collection exists
          if curl -s "http://solr:8983/solr/admin/collections?action=LIST" | grep -q '"news"'; then
            echo "Collection 'news' already exists"
            exit 0
          fi

          echo "Creating collection 'news'..."
          curl -X POST "http://solr:8983/solr/admin/collections?action=CREATE&name=news&numShards=1&replicationFactor=3&collection.configName=news"

          echo "Collection created with 3 replicas"
```

### Component 2: News Crawler (Go Deployment with Shared Queue)

**Purpose**: Continuously crawl URLs from shared queue and index content to Solr

**K8s Resource**: Deployment (stateless workers pulling from Solr-based URL queue)

**Key Features**:
- Shared work queue in Solr (no seed-based sharding)
- True load balancing - any crawler processes any URL
- Atomic claim/release prevents duplicate work
- Scale replicas freely without configuration changes
- Automatic retry of stale claims (crawler died mid-work)

```yaml
apiVersion: apps/v1
kind: Deployment
metadata:
  name: news-crawler
  namespace: digest
spec:
  replicas: 2  # Scale freely; all replicas share the work queue
  selector:
    matchLabels:
      app: news-crawler
  template:
    metadata:
      labels:
        app: news-crawler
    spec:
      containers:
      - name: crawler
        image: ghcr.io/lueurxax/telegram-digest-bot/crawler:latest
        resources:
          requests:
            memory: "256Mi"
            cpu: "100m"
          limits:
            memory: "512Mi"
            cpu: "500m"
        env:
        - name: SOLR_URL
          value: "http://solr:8983/solr/news"
        - name: CRAWL_DEPTH
          value: "2"
        - name: CRAWL_RATE_LIMIT_RPS
          value: "2"
        - name: CRAWL_BATCH_SIZE
          value: "10"
        - name: CRAWL_CLAIM_TTL
          value: "5m"
        - name: CRAWL_USER_AGENT
          value: "DigestBot-Crawler/1.0 (+https://github.com/lueurxax/telegram-digest-bot)"
        - name: POD_NAME
          valueFrom:
            fieldRef:
              fieldPath: metadata.name
        volumeMounts:
        - name: seeds
          mountPath: /config/seeds.txt
          subPath: seeds.txt
        livenessProbe:
          httpGet:
            path: /healthz
            port: 8080
          initialDelaySeconds: 10
          periodSeconds: 30
        readinessProbe:
          httpGet:
            path: /readyz
            port: 8080
          initialDelaySeconds: 5
          periodSeconds: 10
      volumes:
      - name: seeds
        configMap:
          name: crawler-seeds
```

**Why Deployment with Shared Queue:**
- True load balancing: large domains spread across all crawlers automatically
- No configuration changes when scaling (just `kubectl scale`)
- Simpler mental model: crawlers are interchangeable workers
- Fault tolerant: if crawler dies, its claimed URLs become available after TTL

**Index Cleanup CronJob:**

Periodic cleanup of stale pending URLs and old content:

```yaml
apiVersion: batch/v1
kind: CronJob
metadata:
  name: solr-cleanup
  namespace: digest
spec:
  schedule: "0 3 * * *"  # Daily at 3 AM
  concurrencyPolicy: Forbid
  jobTemplate:
    spec:
      template:
        spec:
          restartPolicy: OnFailure
          containers:
          - name: cleanup
            image: curlimages/curl:8.6.0
            command:
            - /bin/sh
            - -c
            - |
              SOLR_URL="http://solr:8983/solr/news"

              # Delete pending URLs older than 7 days
              curl -X POST "$SOLR_URL/update?commit=true" \
                -H "Content-Type: application/json" \
                -d '{"delete":{"query":"crawl_status:pending AND crawled_at:[* TO NOW-7DAYS]"}}'

              # Delete error URLs older than 30 days
              curl -X POST "$SOLR_URL/update?commit=true" \
                -H "Content-Type: application/json" \
                -d '{"delete":{"query":"crawl_status:error AND crawled_at:[* TO NOW-30DAYS]"}}'

              # Delete old content (configurable retention)
              curl -X POST "$SOLR_URL/update?commit=true" \
                -H "Content-Type: application/json" \
                -d '{"delete":{"query":"crawl_status:done AND crawled_at:[* TO NOW-90DAYS]"}}'

              echo "Cleanup complete"
```

**Work Queue Strategy**:

All crawlers share a URL queue stored in Solr. Each URL document has a `crawl_status` field:
- `pending` - Ready to be crawled
- `processing` - Claimed by a crawler (with `crawl_claimed_by` and `crawl_claimed_at`)
- `done` - Successfully crawled and indexed
- `error` - Failed after retries (with `crawl_error` message)

```go
package crawler

import (
    "bufio"
    "bytes"
    "context"
    "crypto/sha256"
    "encoding/hex"
    "encoding/json"
    "fmt"
    "io"
    "net/http"
    "net/url"
    "os"
    "strings"
    "time"

    readability "github.com/go-shiori/go-readability"
    "github.com/rs/zerolog/log"
)

// Config holds crawler configuration
type Config struct {
    SolrURL      string        `env:"SOLR_URL" envDefault:"http://solr:8983/solr/news"`
    CrawlDepth   int           `env:"CRAWL_DEPTH" envDefault:"2"`
    RateLimitRPS float64       `env:"CRAWL_RATE_LIMIT_RPS" envDefault:"2"`
    BatchSize    int           `env:"CRAWL_BATCH_SIZE" envDefault:"10"`
    ClaimTTL     time.Duration `env:"CRAWL_CLAIM_TTL" envDefault:"5m"`
    UserAgent    string        `env:"CRAWL_USER_AGENT" envDefault:"DigestBot-Crawler/1.0"`
    SeedsFile    string        `env:"CRAWL_SEEDS_FILE" envDefault:"/config/seeds.txt"`
    HealthPort   int           `env:"CRAWL_HEALTH_PORT" envDefault:"8080"`
}

// CrawlStatus represents the state of a URL in the queue
type CrawlStatus string

const (
    StatusPending    CrawlStatus = "pending"
    StatusProcessing CrawlStatus = "processing"
    StatusDone       CrawlStatus = "done"
    StatusError      CrawlStatus = "error"
)

// Crawler manages the crawling process using a shared work queue
type Crawler struct {
    cfg         Config
    podName     string
    solr        *SolrClient
    httpClient  *http.Client
    rateLimiter *rate.Limiter // golang.org/x/time/rate
    stopCh      chan struct{}
    doneCh      chan struct{}
}

// NewCrawler creates a new crawler instance
func NewCrawler(cfg Config) (*Crawler, error) {
    podName := os.Getenv("POD_NAME")
    if podName == "" {
        podName = fmt.Sprintf("crawler-%d", os.Getpid())
    }

    log.Info().
        Str("pod", podName).
        Str("solr_url", cfg.SolrURL).
        Msg("crawler initialized")

    return &Crawler{
        cfg:     cfg,
        podName: podName,
        solr:    NewSolrClient(cfg.SolrURL),
        stopCh:  make(chan struct{}),
        doneCh:  make(chan struct{}),
    }, nil
}

// Run starts the crawler loop
func (c *Crawler) Run(ctx context.Context) error {
    defer close(c.doneCh)

    // Start health server
    go c.serveHealth()

    // Seed the queue on startup (idempotent)
    if err := c.seedQueue(ctx); err != nil {
        log.Error().Err(err).Msg("failed to seed queue")
    }

    // Main work loop: claim -> crawl -> release
    for {
        select {
        case <-ctx.Done():
            return ctx.Err()
        case <-c.stopCh:
            return nil
        default:
            if err := c.processNextBatch(ctx); err != nil {
                log.Error().Err(err).Msg("batch processing failed")
                time.Sleep(5 * time.Second) // Back off on error
            }
        }
    }
}

// Stop gracefully stops the crawler
func (c *Crawler) Stop() {
    close(c.stopCh)
    <-c.doneCh
}

// seedQueue adds seed URLs to the queue via RSS/Sitemap discovery (idempotent)
func (c *Crawler) seedQueue(ctx context.Context) error {
    seeds, err := loadSeeds(c.cfg.SeedsFile)
    if err != nil {
        return err
    }

    for _, seed := range seeds {
        baseURL := normalizeToURL(seed)

        // Try RSS/Sitemap first (more efficient than blind crawling)
        urls, err := c.discoverURLs(ctx, baseURL)
        if err != nil {
            log.Warn().Err(err).Str("seed", seed).Msg("discovery failed, using base URL")
            urls = []string{baseURL}
        }

        for _, u := range urls {
            if err := c.enqueueURL(ctx, u, 0); err != nil {
                log.Debug().Err(err).Str("url", u).Msg("failed to enqueue")
            }
        }

        log.Info().Str("seed", seed).Int("urls", len(urls)).Msg("seed discovered")
    }

    return nil
}

// discoverURLs finds article URLs via sitemap, RSS, or homepage crawl
func (c *Crawler) discoverURLs(ctx context.Context, baseURL string) ([]string, error) {
    var urls []string

    // 1. Try sitemap.xml
    sitemapURLs, err := c.fetchSitemap(ctx, baseURL+"/sitemap.xml")
    if err == nil && len(sitemapURLs) > 0 {
        log.Debug().Str("base", baseURL).Int("count", len(sitemapURLs)).Msg("found sitemap")
        return sitemapURLs, nil
    }

    // 2. Try common RSS/Atom feeds
    for _, feedPath := range []string{"/feed", "/rss", "/feed.xml", "/rss.xml", "/atom.xml"} {
        feedURLs, err := c.fetchFeed(ctx, baseURL+feedPath)
        if err == nil && len(feedURLs) > 0 {
            log.Debug().Str("base", baseURL).Str("feed", feedPath).Int("count", len(feedURLs)).Msg("found feed")
            return feedURLs, nil
        }
    }

    // 3. Fallback to base URL (will discover via link crawling)
    return []string{baseURL}, nil
}

// fetchSitemap parses XML sitemap and extracts URLs
func (c *Crawler) fetchSitemap(ctx context.Context, sitemapURL string) ([]string, error) {
    // Implementation: fetch XML, parse <url><loc>...</loc></url> entries
    // Filter to news/article URLs, limit to recent entries
    return nil, fmt.Errorf("not implemented") // Placeholder
}

// fetchFeed parses RSS/Atom feed and extracts article URLs
func (c *Crawler) fetchFeed(ctx context.Context, feedURL string) ([]string, error) {
    // Implementation: fetch XML, parse <item><link>...</link></item> or <entry><link>
    // Use github.com/mmcdole/gofeed for parsing
    return nil, fmt.Errorf("not implemented") // Placeholder
}

// processNextBatch claims and processes a batch of URLs
func (c *Crawler) processNextBatch(ctx context.Context) error {
    // Claim batch of pending URLs
    urls, err := c.claimURLs(ctx, c.cfg.BatchSize)
    if err != nil {
        return fmt.Errorf("claim URLs: %w", err)
    }

    if len(urls) == 0 {
        // No work available, wait before checking again
        time.Sleep(10 * time.Second)
        return nil
    }

    log.Info().Int("count", len(urls)).Msg("processing batch")

    for _, urlDoc := range urls {
        select {
        case <-ctx.Done():
            return ctx.Err()
        default:
            if err := c.crawlURL(ctx, urlDoc); err != nil {
                log.Warn().Err(err).Str("url", urlDoc.URL).Msg("crawl failed")
                c.markError(ctx, urlDoc.ID, err.Error())
            } else {
                c.markDone(ctx, urlDoc.ID)
            }
        }
    }

    return nil
}

// claimURLs atomically claims a batch of pending URLs using optimistic locking
func (c *Crawler) claimURLs(ctx context.Context, count int) ([]URLDoc, error) {
    now := time.Now().UTC()
    staleThreshold := now.Add(-c.cfg.ClaimTTL)

    // Find pending URLs OR stale claims (crawler died)
    // Include _version_ for optimistic concurrency control
    query := url.Values{
        "q":    {"*:*"},
        "fq":   {fmt.Sprintf("crawl_status:(pending OR (processing AND crawl_claimed_at:[* TO %s]))",
                 staleThreshold.Format(time.RFC3339))},
        "rows": {fmt.Sprintf("%d", count)},
        "sort": {"crawl_depth asc, id asc"}, // Prioritize shallow URLs (breadth-first)
        "fl":   {"id,url,domain,crawl_depth,_version_"},
        "wt":   {"json"},
    }

    results, err := c.solr.Search(ctx, query)
    if err != nil {
        return nil, err
    }

    // Atomically claim each URL using optimistic locking
    var claimed []URLDoc
    for _, doc := range results {
        // Conditional update: only succeeds if _version_ matches
        // If another crawler claimed it, version changed and this fails with 409 Conflict
        err := c.solr.ConditionalUpdate(ctx, doc.ID, doc.Version, map[string]interface{}{
            "crawl_status":     map[string]interface{}{"set": StatusProcessing},
            "crawl_claimed_at": map[string]interface{}{"set": now.Format(time.RFC3339)},
            "crawl_claimed_by": map[string]interface{}{"set": c.podName},
        })
        if err != nil {
            // 409 Conflict = version mismatch, another crawler claimed it
            log.Debug().Str("id", doc.ID).Msg("claim conflict, skipping")
            continue
        }
        claimed = append(claimed, doc)
    }

    return claimed, nil
}

// ConditionalUpdate updates a document only if version matches (optimistic locking)
func (c *SolrClient) ConditionalUpdate(ctx context.Context, id string, version int64, fields map[string]interface{}) error {
    doc := map[string]interface{}{
        "id":       id,
        "_version_": version, // Solr checks this - fails with 409 if changed
    }
    for k, v := range fields {
        doc[k] = v
    }

    body, _ := json.Marshal([]interface{}{doc})
    req, err := http.NewRequestWithContext(ctx, http.MethodPost,
        c.baseURL+"/update?commit=true",
        bytes.NewReader(body))
    if err != nil {
        return err
    }
    req.Header.Set("Content-Type", "application/json")

    resp, err := c.httpClient.Do(req)
    if err != nil {
        return err
    }
    defer resp.Body.Close()

    if resp.StatusCode == http.StatusConflict {
        return ErrVersionConflict // Another process updated the document
    }
    if resp.StatusCode != http.StatusOK {
        return fmt.Errorf("solr update failed: %d", resp.StatusCode)
    }

    return nil
}

var ErrVersionConflict = fmt.Errorf("version conflict")

// enqueueURL adds a URL to the queue (idempotent)
func (c *Crawler) enqueueURL(ctx context.Context, rawURL string, depth int) error {
    docID := urlToID(rawURL)

    // Check if already exists
    existing, _ := c.solr.Get(ctx, docID)
    if existing != nil {
        return nil // Already queued
    }

    doc := map[string]interface{}{
        "id":           docID,
        "url":          rawURL,
        "domain":       extractDomain(rawURL),
        "crawl_status": StatusPending,
        "crawl_depth":  depth,
    }

    return c.solr.Index(ctx, doc)
}

// markDone marks URL as successfully crawled
func (c *Crawler) markDone(ctx context.Context, docID string) {
    c.solr.AtomicUpdate(ctx, docID, map[string]interface{}{
        "crawl_status": StatusDone,
        "crawled_at":   time.Now().UTC().Format(time.RFC3339),
    })
}

// markError marks URL as failed
func (c *Crawler) markError(ctx context.Context, docID, errMsg string) {
    c.solr.AtomicUpdate(ctx, docID, map[string]interface{}{
        "crawl_status": StatusError,
        "crawl_error":  errMsg,
    })
}

type URLDoc struct {
    ID         string
    URL        string
    Domain     string
    CrawlDepth int
    Version    int64  // Solr _version_ for optimistic locking
}

// crawlURL fetches a URL, extracts content, indexes it, and discovers new links
func (c *Crawler) crawlURL(ctx context.Context, urlDoc URLDoc) error {
    log.Debug().Str("url", urlDoc.URL).Int("depth", urlDoc.CrawlDepth).Msg("crawling")

    // Fetch and extract content (using colly, goquery, or similar)
    page, err := c.fetchAndExtract(ctx, urlDoc.URL)
    if err != nil {
        return fmt.Errorf("fetch: %w", err)
    }

    // Index the page content
    if err := c.indexPage(ctx, page); err != nil {
        return fmt.Errorf("index: %w", err)
    }

    // Discover and enqueue new links (if within depth limit)
    if urlDoc.CrawlDepth < c.cfg.CrawlDepth {
        for _, link := range page.Links {
            // Only follow same-domain links
            if extractDomain(link) == urlDoc.Domain {
                c.enqueueURL(ctx, link, urlDoc.CrawlDepth+1)
            }
        }
    }

    return nil
}

// fetchAndExtract fetches URL content and extracts structured data
// Uses fallback chain: JSON-LD → RSS/Atom → OpenGraph → Readability
func (c *Crawler) fetchAndExtract(ctx context.Context, rawURL string) (*CrawledPage, error) {
    // Rate limiting per domain
    c.rateLimiter.Wait(ctx)

    // Fetch HTML
    req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
    if err != nil {
        return nil, err
    }
    req.Header.Set("User-Agent", c.cfg.UserAgent)

    resp, err := c.httpClient.Do(req)
    if err != nil {
        return nil, err
    }
    defer resp.Body.Close()

    if resp.StatusCode != http.StatusOK {
        return nil, fmt.Errorf("status %d", resp.StatusCode)
    }

    body, err := io.ReadAll(io.LimitReader(resp.Body, 10*1024*1024)) // 10MB limit
    if err != nil {
        return nil, err
    }

    // Extract content using go-readability (Mozilla Readability algorithm)
    // This removes navigation, ads, boilerplate - extracts main article
    article, err := readability.FromReader(bytes.NewReader(body), rawURL)
    if err != nil {
        return nil, fmt.Errorf("readability: %w", err)
    }

    // Validate content quality
    if len(article.TextContent) < 100 {
        return nil, fmt.Errorf("content too short: %d chars", len(article.TextContent))
    }

    // Detect language (simple heuristic or use external library)
    language := detectLanguage(article.TextContent)

    // Extract links for discovery
    links := extractLinks(body, rawURL)

    return &CrawledPage{
        URL:         rawURL,
        Domain:      extractDomain(rawURL),
        Title:       article.Title,
        Description: article.Excerpt,
        Content:     article.TextContent,
        Language:    language,
        PublishedAt: article.PublishedTime,
        Links:       links,
    }, nil
}

// detectLanguage returns language code based on content analysis
func detectLanguage(text string) string {
    // Use github.com/pemistahl/lingua-go or similar
    // For now, default to unknown (worker will detect later)
    return "unknown"
}

// extractLinks finds all <a href> links in HTML, resolves relative URLs
func extractLinks(html []byte, baseURL string) []string {
    // Use goquery or similar to parse HTML and extract href attributes
    // Filter to same-domain, skip anchors, normalize URLs
    return nil // Placeholder
}

// indexPage indexes a crawled page with language-specific fields
func (c *Crawler) indexPage(ctx context.Context, page *CrawledPage) error {
    doc := map[string]interface{}{
        "id":           urlToID(page.URL),
        "url":          page.URL,
        "domain":       page.Domain,
        "title":        page.Title,
        "description":  page.Description,
        "content":      page.Content,
        "language":     page.Language,
        "crawl_status": StatusDone,
        "crawled_at":   time.Now().UTC().Format(time.RFC3339),
    }

    if page.PublishedAt != nil {
        doc["published_at"] = page.PublishedAt.Format(time.RFC3339)
    }

    // Populate ONLY the matching language-specific dynamic fields
    switch page.Language {
    case "en":
        doc["title_en"] = page.Title
        doc["content_en"] = page.Content
    case "ru":
        doc["title_ru"] = page.Title
        doc["content_ru"] = page.Content
    case "el":
        doc["title_el"] = page.Title
        doc["content_el"] = page.Content
    }

    return c.solr.Index(ctx, doc)
}

// Helper functions
func loadSeeds(path string) ([]string, error) {
    file, err := os.Open(path)
    if err != nil {
        return nil, err
    }
    defer file.Close()

    var seeds []string
    scanner := bufio.NewScanner(file)
    for scanner.Scan() {
        line := strings.TrimSpace(scanner.Text())
        if line == "" || strings.HasPrefix(line, "#") {
            continue
        }
        seeds = append(seeds, line)
    }
    return seeds, scanner.Err()
}

func normalizeToURL(seed string) string {
    if !strings.HasPrefix(seed, "http") {
        return "https://" + seed
    }
    return seed
}

func extractDomain(rawURL string) string {
    u, err := url.Parse(rawURL)
    if err != nil {
        return ""
    }
    return strings.ToLower(u.Host)
}

type CrawledPage struct {
    URL         string
    Domain      string
    Title       string
    Description string
    Content     string
    Language    string     // Detected language: "en", "ru", "el", or "unknown"
    PublishedAt *time.Time
    Links       []string   // Discovered links on the page
}

// SolrClient wraps Solr HTTP API
type SolrClient struct {
    baseURL    string
    httpClient *http.Client
}

// Get fetches a single document by ID using Solr's RealTime Get endpoint
// This is a RAW document fetch - no query parsing, no filters applied
// Used for: checking if URL exists in queue, checking crawl status
func (c *SolrClient) Get(ctx context.Context, id string) (*SolrDoc, error) {
    // Uses /get endpoint which bypasses all query filters
    reqURL := fmt.Sprintf("%s/get?id=%s&wt=json", c.baseURL, url.QueryEscape(id))

    req, err := http.NewRequestWithContext(ctx, http.MethodGet, reqURL, nil)
    if err != nil {
        return nil, err
    }

    resp, err := c.httpClient.Do(req)
    if err != nil {
        return nil, err
    }
    defer resp.Body.Close()

    var result struct {
        Doc *SolrDoc `json:"doc"`
    }
    if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
        return nil, err
    }

    return result.Doc, nil // Returns nil if document not found
}

// Search queries Solr with filters (used by SolrProvider for enrichment)
// Applies crawl_status:done filter to only return indexed content
func (c *SolrClient) Search(ctx context.Context, query url.Values) (*SolrSearchResponse, error) {
    // Uses /select endpoint with all query filters
    reqURL := fmt.Sprintf("%s/select?%s", c.baseURL, query.Encode())
    // ... implementation
    return nil, nil
}

// shouldCrawlPage checks if individual page needs (re)crawling
func (c *Crawler) shouldCrawlPage(ctx context.Context, pageURL string) (bool, error) {
    docID := urlToID(pageURL)

    resp, err := c.solr.Get(ctx, docID)
    if err != nil {
        return true, nil // Crawl on error
    }

    if resp == nil {
        return true, nil // Not crawled yet
    }

    // Check if crawled_at is older than TTL
    if time.Since(resp.CrawledAt) > c.ttl {
        return true, nil // TTL expired, re-crawl
    }

    return false, nil // Recently crawled, skip
}

// serveHealth starts the health check server
func (c *Crawler) serveHealth() {
    mux := http.NewServeMux()
    mux.HandleFunc("/healthz", func(w http.ResponseWriter, r *http.Request) {
        w.WriteHeader(http.StatusOK)
        w.Write([]byte("ok"))
    })
    mux.HandleFunc("/readyz", func(w http.ResponseWriter, r *http.Request) {
        // Check if Solr is reachable
        if c.solr.Ping(r.Context()) {
            w.WriteHeader(http.StatusOK)
            w.Write([]byte("ok"))
        } else {
            w.WriteHeader(http.StatusServiceUnavailable)
            w.Write([]byte("solr unavailable"))
        }
    })

    addr := fmt.Sprintf(":%d", c.cfg.HealthPort)
    log.Info().Str("addr", addr).Msg("starting health server")
    http.ListenAndServe(addr, mux)
}

// canonicalizeURL normalizes URL for consistent hashing
func canonicalizeURL(rawURL string) string {
    u, err := url.Parse(rawURL)
    if err != nil {
        return rawURL
    }

    u.Host = strings.ToLower(u.Host)
    u.Path = strings.TrimSuffix(u.Path, "/")
    u.Fragment = ""

    return u.String()
}

// urlToID generates document ID from canonical URL
func urlToID(rawURL string) string {
    canonical := canonicalizeURL(rawURL)
    hash := sha256.Sum256([]byte(canonical))
    return hex.EncodeToString(hash[:])
}
```

**Scaling Behavior** (shared work queue):

| Replicas | Throughput | Notes |
|----------|------------|-------|
| 1 | Baseline | Single worker processes all URLs |
| 2 | ~2x | Work split automatically via queue |
| N | ~Nx | Linear scaling up to Solr bottleneck |

**Benefits of shared queue:**
- No configuration changes when scaling (`kubectl scale deployment news-crawler --replicas=N`)
- Large domains automatically spread across all workers
- Failed crawlers' work is automatically redistributed after claim TTL expires

### Component 3: Search Adapter (Provider Interface)

**Purpose**: Implement existing `Provider` interface to query Solr

**Location**: `internal/process/enrichment/provider_solr.go`

```go
package enrichment

import (
    "context"
    "encoding/json"
    "fmt"
    "net/http"
    "net/url"
    "strconv"
    "time"
)

type SolrProvider struct {
    baseURL    string
    httpClient *http.Client
    timeout    time.Duration
}

type SolrConfig struct {
    Enabled    bool          `env:"SOLR_ENABLED" envDefault:"false"`
    BaseURL    string        `env:"SOLR_URL" envDefault:"http://solr:8983/solr/news"`
    Timeout    time.Duration `env:"SOLR_TIMEOUT" envDefault:"10s"`
    MaxResults int           `env:"SOLR_MAX_RESULTS" envDefault:"10"`
}

func NewSolrProvider(cfg SolrConfig) *SolrProvider {
    return &SolrProvider{
        baseURL:    cfg.BaseURL,
        httpClient: &http.Client{Timeout: cfg.Timeout},
        timeout:    cfg.Timeout,
    }
}

func (p *SolrProvider) Name() ProviderName {
    return ProviderSolr
}

func (p *SolrProvider) Search(ctx context.Context, query, language string, maxResults int) ([]SearchResult, error) {
    solrQuery := url.Values{
        "q":       {query},
        "rows":    {strconv.Itoa(maxResults)},
        "fl":      {"id,url,title,description,content,domain,language,published_at"},
        "defType": {"edismax"},
        "wt":      {"json"},
    }

    // Only search successfully crawled documents (not pending/error)
    solrQuery.Add("fq", "crawl_status:done")

    // Language filter: only apply if specified and known
    // Documents are indexed with language-specific dynamic fields (title_en, content_ru, etc.)
    if language != "" && language != "auto" && language != "unknown" {
        // Filter to matching language only (no "unknown" fallback to avoid wrong stemmer results)
        solrQuery.Add("fq", fmt.Sprintf("language:%s", language))

        // Boost language-specific dynamic fields when available
        switch language {
        case "en":
            solrQuery.Set("qf", "title_en^3.0 title^2.0 content_en^1.5 content^1.0 description^1.0")
        case "ru":
            solrQuery.Set("qf", "title_ru^3.0 title^2.0 content_ru^1.5 content^1.0 description^1.0")
        case "el":
            solrQuery.Set("qf", "title_el^3.0 title^2.0 content_el^1.5 content^1.0 description^1.0")
        default:
            solrQuery.Set("qf", "title^2.0 description^1.5 content^1.0")
        }
    } else {
        // No language filter - search all documents with general fields
        solrQuery.Set("qf", "title^2.0 description^1.5 content^1.0")
    }

    // Execute query
    reqURL := fmt.Sprintf("%s/select?%s", p.baseURL, solrQuery.Encode())
    req, err := http.NewRequestWithContext(ctx, http.MethodGet, reqURL, nil)
    if err != nil {
        return nil, fmt.Errorf("create request: %w", err)
    }

    resp, err := p.httpClient.Do(req)
    if err != nil {
        return nil, fmt.Errorf("solr request: %w", err)
    }
    defer resp.Body.Close()

    if resp.StatusCode != http.StatusOK {
        return nil, fmt.Errorf("solr returned status %d", resp.StatusCode)
    }

    var solrResp solrResponse
    if err := json.NewDecoder(resp.Body).Decode(&solrResp); err != nil {
        return nil, fmt.Errorf("decode response: %w", err)
    }

    return p.mapResults(solrResp), nil
}

func (p *SolrProvider) mapResults(resp solrResponse) []SearchResult {
    results := make([]SearchResult, 0, len(resp.Response.Docs))
    for _, doc := range resp.Response.Docs {
        results = append(results, SearchResult{
            URL:         doc.URL,
            Title:       doc.Title,
            Description: doc.Description,
            Content:     doc.Content,
            Domain:      doc.Domain,
            Language:    doc.Language,
            PublishedAt: doc.PublishedAt,
        })
    }
    return results
}

func (p *SolrProvider) IsAvailable(ctx context.Context) bool {
    reqURL := fmt.Sprintf("%s/admin/ping", p.baseURL)
    req, err := http.NewRequestWithContext(ctx, http.MethodGet, reqURL, nil)
    if err != nil {
        return false
    }

    resp, err := p.httpClient.Do(req)
    if err != nil {
        return false
    }
    defer resp.Body.Close()

    return resp.StatusCode == http.StatusOK
}

type solrResponse struct {
    Response struct {
        NumFound int       `json:"numFound"`
        Docs     []solrDoc `json:"docs"`
    } `json:"response"`
}

type solrDoc struct {
    ID          string    `json:"id"`
    URL         string    `json:"url"`
    Title       string    `json:"title"`
    Description string    `json:"description"`
    Content     string    `json:"content"`
    Domain      string    `json:"domain"`
    Language    string    `json:"language"`
    PublishedAt time.Time `json:"published_at"`
}
```

### Component 4: Telegram Index (Future Enhancement)

**Purpose**: Index Telegram messages for cross-channel evidence verification

**Rationale**: Telegram is often faster than traditional news sources, especially for regional content (Cyprus, Russia, Ukraine). Cross-referencing claims across multiple unrelated channels provides additional corroboration.

**Architecture**:
```
┌─────────────────┐     ┌─────────────────┐     ┌─────────────────┐
│  Reader (MTProto)│────▶│   PostgreSQL    │     │                 │
│  Ingests messages│     │ (current store) │     │                 │
└────────┬────────┘     └─────────────────┘     │      Solr       │
         │                                       │                 │
         │  dual-write                          │  source:web     │
         │                                       │  source:telegram│
         ▼                                       │                 │
┌─────────────────┐                             │                 │
│  Telegram Indexer│─────────────────────────────▶                 │
│  (in Reader)     │                             └─────────────────┘
└─────────────────┘
```

**Schema Extension** (add to existing Solr schema):
```xml
<!-- Source type: web or telegram -->
<field name="source" type="string" indexed="true" stored="true" default="web"/>

<!-- Telegram-specific fields -->
<field name="tg_peer_id" type="plong" indexed="true" stored="true"/>
<field name="tg_channel_username" type="string" indexed="true" stored="true"/>
<field name="tg_message_id" type="pint" indexed="true" stored="true"/>
<field name="tg_views" type="pint" indexed="true" stored="true"/>
<field name="tg_forwards" type="pint" indexed="true" stored="true"/>
```

**Shared Helper** (used by both Reader and Worker):
```go
// package solrindex - shared between reader and worker

// TelegramDocID computes the Solr document ID for a Telegram message.
// Uses the same canonical URL format as web pages for consistent hashing.
func TelegramDocID(tgPeerID int64, messageID int) string {
    canonicalURL := fmt.Sprintf("tg://peer/%d/msg/%d", tgPeerID, messageID)
    return urlToID(canonicalURL) // SHA256 hash
}

// TelegramDisplayURL builds a user-facing URL for a Telegram message.
// For public channels: https://t.me/username/123
// For private channels: tg://channel/123456/789 (non-clickable, for reference only)
func TelegramDisplayURL(tgPeerID int64, username string, messageID int) string {
    if username != "" {
        return fmt.Sprintf("https://t.me/%s/%d", username, messageID)
    }
    // Private channel - use tg:// scheme (not a valid web URL, clearly non-clickable)
    // Note: t.me/c/<id> requires the *channel ID* (peer_id without -100 prefix),
    // but that only works for users who are members. Use tg:// for clarity.
    return fmt.Sprintf("tg://channel/%d/%d", tgPeerID, messageID)
}
```

**Indexing from Reader**:
```go
// In reader, after storing message to PostgreSQL
// Note: Language detection happens later in pipeline; default to "unknown" at ingest
func (r *Reader) indexToSolr(ctx context.Context, msg *Message, channel *Channel) error {
    docID := solrindex.TelegramDocID(channel.TgPeerID, msg.ID)
    displayURL := solrindex.TelegramDisplayURL(channel.TgPeerID, channel.Username, msg.ID)

    doc := map[string]interface{}{
        "id":                  docID,
        "source":              "telegram",
        "url":                 displayURL,
        "domain":              "t.me", // Keep domain consistent (DNS hostname)
        "title":               truncate(msg.Text, 100),
        "content":             msg.Text,
        "language":            "unknown", // Detected later by worker, updated via atomic update
        "published_at":        msg.Date,
        "crawl_status":        "done", // Already "crawled" by reader
        "crawled_at":          time.Now(),
        "tg_peer_id":          channel.TgPeerID, // Numeric Telegram peer ID
        "tg_channel_username": channel.Username, // May be empty for private channels
        "tg_message_id":       msg.ID,
        "tg_views":            msg.Views,
        "tg_forwards":         msg.Forwards,
    }

    return r.solr.Index(ctx, doc)
}
```

**Language Update from Worker**:
```go
// In worker, after language detection
// Worker has item with tg_peer_id and tg_message_id from PostgreSQL
func (w *Worker) updateTelegramLanguage(ctx context.Context, item *Item, language string) error {
    // Compute same docID that Reader used
    docID := solrindex.TelegramDocID(item.TgPeerID, item.TgMessageID)

    // Atomic update - only changes the language field
    return w.solr.AtomicUpdate(ctx, docID, map[string]interface{}{
        "language": map[string]interface{}{"set": language},
    })
}
```

**Evidence Search with Telegram**:
```go
// In SolrProvider.Search, include both sources
func (p *SolrProvider) Search(ctx context.Context, query, language string, maxResults int) ([]SearchResult, error) {
    solrQuery := url.Values{
        "q":    {query},
        "rows": {strconv.Itoa(maxResults)},
    }

    // Filter queries - use Add() to allow multiple fq params
    solrQuery.Add("fq", "crawl_status:done") // Works for both web and telegram

    // Optionally filter by source (default: both)
    // solrQuery.Add("fq", "source:(web OR telegram)")

    // Filter by Telegram channel if needed
    // solrQuery.Add("fq", "tg_channel_username:CyprusNews")

    // ... rest of search logic
}
```

**Message Lifecycle** (deletions/edits):
- **Immutable by default**: Once indexed, messages stay in Solr even if deleted on Telegram
- **Rationale**: Evidence should be preserved; deleted messages may still be relevant for fact-checking
- **Optional soft-delete**: Add `tg_deleted: true` field if deletion tracking is needed later
- **Edits**: Not tracked; original message preserved (Telegram edit history is limited anyway)

**Benefits**:
- Cross-channel verification (5 channels report same event → higher confidence)
- No external API costs (already have MTProto)
- Real-time (Telegram breaks news before traditional media)
- Same language/context (Russian Telegram finds Russian sources)

**Future: Solr as Primary Store**:
Once Telegram indexing is proven, consider migrating message storage entirely to Solr:
- Solr supports all current query patterns
- Unified search across web + Telegram
- Simpler architecture (one store instead of two)
- Trade-off: PostgreSQL better for relational queries, Solr better for full-text

## Migration Plan

### Phase 1: Deploy Solr (Week 1)

1. Apply ConfigMap with schema and solrconfig
2. Deploy StatefulSet with init container
3. Verify collection is created and healthy
4. Keep YaCy running in parallel

**Verification**:
```bash
# Check SolrCloud cluster status
kubectl exec -n digest solr-0 -- curl -s 'http://localhost:8983/solr/admin/collections?action=CLUSTERSTATUS' | jq '.cluster.live_nodes'

# Check collection health (all 3 replicas active)
kubectl exec -n digest solr-0 -- curl -s 'http://localhost:8983/solr/admin/collections?action=CLUSTERSTATUS' | jq '.cluster.collections.news.shards.shard1.replicas | to_entries[] | {node: .value.node_name, state: .value.state}'

# Verify schema fields
kubectl exec -n digest solr-0 -- curl -s 'http://localhost:8983/solr/news/schema/fields' | jq '.fields[].name'

# Check ZooKeeper ensemble
for i in 0 1 2; do echo "zk-$i:"; kubectl exec -n digest zookeeper-$i -- sh -c 'echo stat | nc localhost 2181 | grep Mode'; done
```

### Phase 2: Deploy Crawler (Week 1-2)

1. Implement crawler in Go (new `cmd/crawler/` binary)
2. Deploy as Deployment with shared work queue
3. Verify queue processing (seeds enqueued, URLs claimed and indexed)
4. Monitor indexing progress via logs and Solr queue status

**Verification**:
```bash
# Check indexed document count (crawl_status:done)
kubectl exec -n digest solr-0 -- curl -s 'http://localhost:8983/solr/news/select?q=*:*&fq=crawl_status:done&rows=0' | jq '.response.numFound'

# Check queue status
kubectl exec -n digest solr-0 -- curl -s 'http://localhost:8983/solr/news/select?q=*:*&rows=0&facet=true&facet.field=crawl_status' | jq '.facet_counts.facet_fields.crawl_status'

# Test search
kubectl exec -n digest solr-0 -- curl -s 'http://localhost:8983/solr/news/select?q=cyprus+news&fq=crawl_status:done&rows=5' | jq '.response.docs[].title'

# Check crawl coverage by domain
kubectl exec -n digest solr-0 -- curl -s 'http://localhost:8983/solr/news/select?q=*:*&fq=crawl_status:done&rows=0&facet=true&facet.field=domain'

# Check crawler logs (queue processing)
kubectl logs -n digest -l app=news-crawler --tail=50 | grep "processing batch"

# Check crawler Deployment status
kubectl get deployment -n digest news-crawler
kubectl get pods -n digest -l app=news-crawler
```

### Phase 3: Integrate Search Adapter (Week 2)

1. Implement `SolrProvider` in enrichment package
2. Add configuration options
3. Add to provider registry (priority before SearXNG)
4. Deploy and verify evidence extraction

### Phase 4: Deprecate YaCy (Week 3)

1. Monitor Solr-based enrichment for 1 week
2. Compare evidence extraction rates
3. Scale YaCy to 0 replicas
4. Remove YaCy manifests after 2 weeks stable

## Configuration

### Environment Variables

```bash
# Solr Provider (for enrichment workers)
SOLR_ENABLED=true
SOLR_URL=http://solr:8983/solr/news
SOLR_TIMEOUT=10s
SOLR_MAX_RESULTS=10

# Crawler Deployment (shared work queue)
SOLR_URL=http://solr:8983/solr/news
CRAWL_DEPTH=2                  # Max link depth from seed
CRAWL_RATE_LIMIT_RPS=2         # Requests per second (per crawler)
CRAWL_BATCH_SIZE=10            # URLs to claim per batch
CRAWL_CLAIM_TTL=5m             # Time before stale claims are reclaimed
CRAWL_USER_AGENT="DigestBot-Crawler/1.0"
CRAWL_SEEDS_FILE="/config/seeds.txt"
CRAWL_HEALTH_PORT=8080

# Pod identity (auto-injected)
POD_NAME=news-crawler-xxx      # Used for claim tracking
```

### Seed Domains (ConfigMap)

Expanded from YaCy seeds to reach 30 domains for target coverage:
```yaml
apiVersion: v1
kind: ConfigMap
metadata:
  name: crawler-seeds
  namespace: digest
data:
  seeds.txt: |
    # International News (8 domains)
    reuters.com
    apnews.com
    bbc.com
    aljazeera.com
    dw.com
    france24.com
    euronews.com
    theguardian.com

    # Science & Tech (4 domains)
    nature.com
    wired.com
    arstechnica.com
    sciencemag.org

    # Ukraine/Russia (6 domains)
    pravda.com.ua
    ukrinform.net
    suspilne.media
    unian.net
    meduza.io
    novayagazeta.eu

    # Cyprus (8 domains)
    cyprus-mail.com
    philenews.com
    sigmalive.com
    in-cyprus.philenews.com
    reporter.com.cy
    politis.com.cy
    stockwatch.com.cy
    financialmirror.com

    # International orgs (4 domains)
    who.int
    un.org
    europa.eu
    imf.org
```

**Total: 30 seed domains** → 30 × 100 pages/domain = 3,000 documents/week target

## Resource Comparison

| Component | Replicas | Memory Request | Memory Limit | CPU Request | CPU Limit |
|-----------|----------|----------------|--------------|-------------|-----------|
| **YaCy (Current)** | 1 | 6Gi | 12Gi | 250m | 2000m |
| **ZooKeeper (Proposed)** | 3 | 1.5Gi (3×512Mi) | 3Gi (3×1Gi) | 300m | 1500m |
| **SolrCloud (Proposed)** | 3 | 6Gi (3×2Gi) | 12Gi (3×4Gi) | 750m | 3000m |
| **Crawler (Proposed)** | 2 | 512Mi | 1Gi | 200m | 1000m |
| **Total (Proposed)** | - | 8Gi | 16Gi | 1250m | 5500m |

**Trade-off**: More resources than YaCy, but gains HA (no single point of failure), persistent index, and independent scaling.

**Minimum cluster**: 3 nodes with 6GB each, or 2 nodes with 10GB each.

**Crawler Scaling**: Deployment with N replicas; each adds 256Mi request / 512Mi limit.

## Advantages

### Operational
- **Stable memory footprint**: Solr has predictable memory usage with explicit heap sizing
- **High availability**: 3-node SolrCloud with ZooKeeper; tolerates node failures
- **Persistent index**: StatefulSet with PVC survives restarts
- **Independent scaling**: Crawler and index scale separately
- **True load balancing**: Shared work queue distributes URLs evenly regardless of domain size
- **Simple scaling**: Just `kubectl scale` - no configuration changes needed
- **Fault tolerant**: Dead crawlers' work automatically redistributed after claim TTL
- **Standard tooling**: Solr has mature monitoring, backup, and admin tools

### Performance
- **Faster queries**: Solr optimized for search (vs YaCy's P2P overhead)
- **Better relevance**: Configurable scoring, boosting, and ranking
- **Language support**: Per-language analyzers for EN/RU/EL stemming

### Maintainability
- **Smaller codebase**: Crawler is ~500 lines of Go vs YaCy's Java monolith
- **Familiar stack**: Go + Solr vs Java P2P networking
- **Easier debugging**: Clear separation of concerns

## Risks and Mitigations

| Risk | Likelihood | Impact | Mitigation |
|------|------------|--------|------------|
| Solr learning curve | Medium | Low | Well-documented, standard REST API |
| Crawler bugs | Medium | Medium | Start with simple implementation, iterate |
| Index corruption | Low | High | PVC snapshots via cloud provider backup |
| Migration downtime | Low | Medium | Run in parallel, gradual cutover |

**Note**: 3-node SolrCloud provides high availability. With replicationFactor=3, the cluster can tolerate 1 node failure without data loss or downtime.

## Alternatives Considered

### 1. Elasticsearch instead of Solr
- **Pros**: More features, better scaling
- **Cons**: Higher resource usage (4GB+ minimum), more complex
- **Decision**: Solr is sufficient for our scale, lower resource requirements

### 2. Meilisearch
- **Pros**: Very fast, easy setup, low memory
- **Cons**: Less mature, limited language support for RU/EL
- **Decision**: Consider for future if Solr is overkill

### 3. Fix YaCy configuration
- **Pros**: No migration needed
- **Cons**: Fundamental architecture issues remain, memory problems inherent
- **Decision**: Already tried multiple configurations, issues persist

### 4. Use only external APIs (SearXNG, GDELT, NewsAPI)
- **Pros**: No local index to maintain
- **Cons**: Rate limits, costs, availability dependencies
- **Decision**: Local index provides reliability and no rate limits

## Success Criteria

1. **Stability**: Zero OOMKilled events for 2 weeks
2. **Index persistence**: Index survives pod restarts
3. **Search quality**: Evidence extraction rate >= current (14%)
4. **Resource efficiency**: Runs on 3-node cluster (3×6GB nodes minimum; 8Gi total request, 16Gi total limit)
5. **Crawl coverage**: 3,000+ documents indexed within 1 week (30 seeds × 100 pages/domain)

## Timeline

| Week | Milestone |
|------|-----------|
| 1 | Deploy Solr with schema, verify collection works |
| 1-2 | Implement and deploy crawler |
| 2 | Implement SolrProvider, integrate with workers |
| 3 | Monitor, tune, deprecate YaCy |
| 4 | Remove YaCy manifests, document |

## Resolved Questions

| Question | Decision |
|----------|----------|
| **Solr mode** | SolrCloud with 3 replicas + ZooKeeper for high availability |
| **Work distribution** | Shared URL queue in Solr with `crawl_status` field; no seed-based sharding |
| **Multi-language strategy** | Dynamic fields populated at index time based on detected language; no copyFields |
| **Unique document key** | SHA256 hash of canonical URL (lowercase host, no trailing slash, no fragment) |
| **Crawler deployment type** | Deployment (stateless); all replicas pull from shared queue |
| **Load balancing** | Automatic via queue; large domains spread across all crawlers |
| **Fault tolerance** | Stale claims (crawler died) reclaimed after `CRAWL_CLAIM_TTL` |
| **Claim exclusivity** | Solr `_version_` field for optimistic concurrency control (409 on conflict) |
| **Get vs Search API** | `solr.Get` uses `/get` endpoint (raw ID fetch); `solr.Search` uses `/select` with `fq=crawl_status:done` |
| **Telegram messages** | Immutable once indexed; deletions/edits not synced (evidence preservation) |
| **Telegram URL format** | Canonical: `tg://peer/<peer_id>/msg/<msg_id>` (for ID hash); Display: `https://t.me/<username>/<id>` (public) or `tg://channel/<peer_id>/<id>` (private, non-clickable) |
| **Telegram language update** | Shared `solrindex.TelegramDocID()` helper; Worker computes same docID from `item.TgPeerID` + `item.TgMessageID` |

## Open Questions

1. ~~**Crawl frequency**: Hourly sufficient or need continuous?~~ **Resolved**: Crawlers run continuously, pulling from shared queue. No fixed interval - work processed as fast as rate limits allow.
2. **Index retention**: How long to keep old documents? (Propose: 30 days, then delete via Solr TTL policy)
3. **Backup strategy**: PVC snapshots via cloud provider? (Recommend: Daily snapshots)

## Gaps, Risks & Mitigations

### Risk Matrix

| Risk | Likelihood | Impact | Mitigation |
|------|------------|--------|------------|
| **ZooKeeper quorum loss** | Low | High | 3-node ensemble tolerates 1 failure; monitoring |
| **Crawl coverage gaps** | Medium | Medium | RSS/sitemap support; expandable seed list; monitoring |
| **Content extraction noise** | Medium | Medium | Readability-based extraction; boilerplate removal |
| **Index bloat (pending URLs)** | Low | Low | Periodic cleanup job; TTL for stale entries |
| **Solr learning curve** | Medium | Low | Well-documented; standard REST API |
| **Site-specific crawler failures** | Medium | Low | Per-site error monitoring; graceful degradation |

### High Availability (Solr)

**Architecture**: 3-node SolrCloud with ZooKeeper eliminates single point of failure.

**Properties**:
1. **Fault tolerance** - cluster survives 1 node failure (quorum = 2/3)
2. **Data durability** - replicationFactor=3 means data on all nodes
3. **Automatic failover** - ZooKeeper handles leader election
4. **Rolling updates** - can upgrade nodes one at a time

**Monitoring**: Alert on replica count < 3, ZK quorum loss, query latency spikes, heap usage > 80%

### Crawl Coverage & Freshness

**Risk**: Fixed seed list (30 domains) may miss important sources; high-volume sites may exceed 100 pages/week cap.

**Mitigations**:
1. **RSS/Sitemap discovery** - fetch `/sitemap.xml`, `/feed`, `/rss` for efficient new article discovery
2. **Dynamic limits** - adjust `CRAWL_MAX_PAGES_PER_DOMAIN` per site based on volume
3. **Source curation process** - quarterly review of seed list
4. **Monitoring**: Track pages indexed per domain; alert if major site stops updating

### Content Extraction Quality

**Risk**: Crawler may index navigation, ads, boilerplate instead of article text.

**Mitigations**:
1. **Readability algorithm** - use go-readability or similar for main content extraction
2. **Fallback chain**: JSON-LD → RSS/Atom → OpenGraph → Readability → raw text
3. **Field validation** - reject documents with < 100 chars content
4. **Quality sampling** - periodic manual review of indexed content

### Index Maintenance

**Risk**: Pending/error URLs accumulate; stale content lingers.

**Mitigations**:
1. **Cleanup CronJob** - purge URLs in `pending` status > 7 days
2. **Error retry limit** - max 3 retries, then permanent `error` status
3. **Content TTL** - delete documents with `crawled_at` > 90 days (configurable)
4. **Dedup monitoring** - alert if duplicate URLs detected (canonicalization failure)

### Operational Monitoring

**Key Metrics to Track**:

| Metric | Source | Alert Threshold |
|--------|--------|-----------------|
| Solr heap usage | JMX/Prometheus | > 80% |
| Query latency p99 | Solr metrics | > 500ms |
| Crawl errors/hour | Crawler logs | > 50 |
| Documents indexed/day | Solr stats | < 100 (stalled) |
| Pending queue size | Solr query | > 10,000 |
| Coverage per domain | Solr facet | 0 for any seed (site broken) |

**Recommended Stack**: Prometheus + Grafana with Solr exporter

---

## Future Enhancements (Out of Scope)

These items are valuable but deferred to future iterations:

1. **Clustering/MLT** - Group related articles using Solr's MoreLikeThis or Carrot2
2. **Fact-check sources** - Add Snopes, PolitiFact, FactCheck.org to seeds
3. **Stance analysis** - Tag articles as pro/contra on topics via enrichment
4. **Semantic search** - Vector embeddings for similarity (Solr 9.x supports dense vectors)
5. **User feedback** - Thumbs up/down on results to tune relevance
6. **Multi-shard scaling** - Add shards when index exceeds single-node capacity

---

## References

- [Apache Solr Documentation](https://solr.apache.org/guide/)
- [Solr Schema Design](https://solr.apache.org/guide/solr/latest/indexing-guide/schema-elements.html)
- [Solr Language Analysis](https://solr.apache.org/guide/solr/latest/indexing-guide/language-analysis.html)
- [Solr Prometheus Exporter](https://solr.apache.org/guide/solr/latest/deployment-guide/prometheus-exporter.html)
- [go-readability](https://github.com/go-shiori/go-readability) - Content extraction (Mozilla Readability)
- [gofeed](https://github.com/mmcdole/gofeed) - RSS/Atom feed parsing
- [lingua-go](https://github.com/pemistahl/lingua-go) - Language detection
- [Colly Go Crawler Framework](https://github.com/gocolly/colly)
- [Current YaCy Issues](#problem-statement)
