# RESEARCH

First Principles are discussed about Observability v2.0 in this article: 

See how close the implementation of FrostDB is to Scuba implementation --> https://github.com/polarsignals/frostdb

Try out the SlateDB as well slatedb-go.  It is LSM which is gathered up into Parquet format in S3? How about query immediately?

FrostDB uses LSM; with Level-0 being Apache Arrow and once gathered up; sent using Parquet

InfluxDB v3; it is time-series specific. When ingesting; there is a catalog which tells min-max for the segments.
    WAL is there to handle crashes before it gets into storage.  It is only single-node for now.

ClickHouse uses a single-level LSM during ingestion; materialization is able to match the query pattern.  Depending on the query can select the correct algo.

VictoriaMetrics is able to use its existing component to form up an app VictoriaLogs which apparently meet what we need?

Using NATS as light-weight moduler monolith - https://kaustavdm.medium.com/ultra-lightweight-nats-based-modular-app-framework-in-go-860d210f46de

Using httpz as a more strucutred endpoint .. chi + echo 

Full OpenAPI v3 implementaiton + validaiton ..

- Huma - 
- dd
- ddd
- ddd

Test with Bruno ..