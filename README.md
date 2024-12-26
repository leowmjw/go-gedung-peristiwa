# go-gedung-peristiwa
Column Store of Events (CloudEvents)

For external or business context events; ensure it is in the CLoudEvents format; use SDKs

Use OpenTelemetry for tracing flow from outside in; using the common spanID. Follow wide event idea as much as possible; see Boris tane article.

The proxy will receive event or start a OpenTelemetry trace or collect existing span to be emitted; where the fields

This will act as Bronze layer; data are appended to be immutable; easy to be purged after fixed time (regulation)
Ideally if can be flexible on the timing; and can be reconstructed if needed?

SQLMesh will start with the Bronze layer and track the transformation from there; leveraging S3 Tables.  
GlareDB can help in combining? Or transform with Spark v4 Comet? Or loaded into frame with Daft?

Sensitive fields marked by source (and auto detected)
