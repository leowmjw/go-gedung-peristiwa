# Gedung Peristiwa — Product Requirements Document

> **Gedung Peristiwa** (Indonesian: "Event House") — A FinTech data simplification layer.

## Vision

Simplify FinTech data infrastructure. Instead of operating Kafka + Spark + custom event trackers + ETL pipelines, run one Go binary with IsleDB writing to Tigris.

The modern FinTech data stack is over-engineered for what most teams actually need: reliably write events, read them back in order, fan out to multiple consumers, and deduplicate. Gedung Peristiwa delivers all of this as a single, embeddable Go library backed by object storage.

## Problem Statement

FinTech companies face three recurring data problems:

### Data Silos

Each team operates its own Kafka topics, databases, and pipelines. Loan origination, payment processing, and fraud detection each maintain separate copies of overlapping event streams. Integration between silos requires bespoke connectors and constant maintenance.

### Data Drift

Schemas change without coordination. Consumers fall behind producers. Reconciliation between systems becomes a permanent, manual process. Teams lose confidence in their data and build yet another pipeline to "get the real numbers."

### Data Duplication

The same business events are ingested, transformed, and stored by multiple independent pipelines. Deduplication is either hand-rolled (fragile) or ignored (expensive). Storage costs grow linearly with the number of consumers.

### Why Current Solutions Fall Short

The standard toolbox — Kafka, Spark, Flink, Airflow — solves each problem individually but introduces significant operational complexity:

- Kafka requires dedicated cluster management, topic governance, and consumer group coordination.
- Spark/Flink jobs need scheduling, monitoring, and failure recovery infrastructure.
- Airflow DAGs become tangled dependency graphs that are difficult to reason about.
- Each tool has its own deployment model, scaling characteristics, and failure modes.

For many FinTech teams, the operational cost of running this stack exceeds the value it delivers.

## Solution

Gedung Peristiwa is a single Go library layer built on [IsleDB](https://github.com/ankur-anand/isledb) and object storage.

### Architecture

IsleDB implements an LSM-tree adapted for object storage:

- **Single writer** flushes an in-memory memtable → SST files → object storage.
- **Multiple readers** can read independently from the same object store.
- **Tailing readers** stream new writes in real time, like `tail -f`.

### How It Maps to the Current Stack

| Current Tool | Gedung Peristiwa Equivalent | Mechanism |
|---|---|---|
| Kafka producer | IsleDB writer | Single writer ingests events into memtable |
| Kafka brokers + S3 sinks | Tigris object store | Memtable flushes SST files to S3-compatible storage |
| Kafka consumers | IsleDB tailing readers | Fan-out via independent readers streaming from object store |
| Custom dedup logic | LSM compaction | Compaction merges duplicate keys automatically |
| Event sourcing infra | Manifest log | Ordered replay via IsleDB's manifest-based log |

### Key Properties From IsleDB

- **No data silos**: Single writer is the source of truth. Horizontal readers consume the same data independently without connectors.
- **No data drift**: Manifest fencing and epoch-based ownership prevent split-brain writes. Readers always see a consistent view.
- **No data duplication**: LSM compaction merges duplicate keys during background maintenance. Storage cost stays proportional to unique data.

### Storage Tiers

| Environment | Object Store | Notes |
|---|---|---|
| Local dev / MVP | MinIO | Local S3-compatible store, no cloud dependencies |
| Production | Tigris | S3-compatible, FoundationDB-based, globally distributed |

## MVP Scope

The MVP proves the core write → flush → read → tail pipeline locally (MinIO), then validates the same pipeline against Tigris (S3 API) with read-back verification.

### What It Does

1. **Multi-tenant event generation** — Simulates 1000 events over 60 seconds across multiple tenants.
2. **Realistic traffic patterns** — Includes spikes, plateaus, and varying per-tenant throughput.
3. **Write → flush → read → tail pipeline** — Events written via IsleDB, flushed to object storage (MinIO then Tigris), consumed by tailing readers.
4. **Event ordering guarantees** — Unique event keys (UUID v7 idempotency keys) are observed in lexicographic (time) order within each tenant prefix.
5. **Basic deduplication via compaction** — LSM compaction merges duplicate keys, demonstrating automatic dedup.
6. **Tigris validation** — After MinIO passes, run the same simulation against a Tigris bucket and confirm data correctness (counts, ordering, dedup).

### What It Requires

- Go binary (single process)
- MinIO (local S3-compatible object store), managed by overmind
- Tigris account credentials (for optional `simulate-tigris` step only)

### What It Does NOT Require

- No container orchestration
- No external databases beyond object storage
- MinIO path: no cloud credentials; network limited to localhost

## Success Criteria

| Criterion | Verification |
|---|---|
| All 1000 events written and readable | Reader can retrieve every unique event by key |
| Tailing reader receives events in correct order | Unique UUID v7 keys are lexicographically non-decreasing within tenant+type prefix |
| Compaction reduces duplicate keys | Post-compaction unique key count < pre-compaction put count when duplicates exist |
| No data loss across writer/reader boundary | Unique reader key count == unique generated events (after compaction) |
| Runs fully standalone (MinIO) | `overmind start` brings up MinIO + simulation with no manual steps |
| Tigris data matches MinIO behavior | `simulate-tigris` passes same verification against Tigris bucket |

## Non-Goals (MVP)

The following are explicitly out of scope for the MVP:

- **Multi-region / production Tigris operations** — MVP only writes to one Tigris bucket and verifies data; not full prod rollout.
- **Multi-node IsleDB** — Single writer, single process only.
- **Authentication / authorization** — No tenant isolation or access control.
- **Schema registry** — Events are opaque key-value pairs.
- **Real FinTech data formats** — Simulated events only; no PCI/PII concerns.

## Future Vision

Beyond the MVP, Gedung Peristiwa aims to become a general-purpose replacement for heavyweight data infrastructure in append-heavy workloads.

### Replace Kafka for Append-Heavy Workloads

For use cases where events are written once and read many times — audit logs, transaction streams, activity feeds — IsleDB on object storage offers the same semantics with dramatically lower operational burden.

### Tigris Global Distribution

Tigris provides FoundationDB-backed global distribution out of the box. Multi-region event streams become a storage configuration change, not an infrastructure project.

### Retention Policies for Compliance

IsleDB's manifest log enables time-based and size-based retention policies. FinTech compliance requirements (data retention, right to erasure) can be enforced at the storage layer.

### CDC Pipeline Buffer

Gedung Peristiwa can serve as a buffer between CDC sources (database change streams) and downstream consumers, replacing Kafka Connect + Kafka for change data capture patterns.

### Lakehouse Landing Zone

SST files on object storage are already in a format amenable to analytical queries. Gedung Peristiwa can serve as a landing zone for lakehouse architectures, replacing the Kafka → S3 sink connector → Spark ETL chain.
