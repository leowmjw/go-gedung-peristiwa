minio: minio server ./data/minio --address :9000 --console-address :9001
setup: mise run minio-setup
simulate: go run ./cmd/simulate/ --backend minio
