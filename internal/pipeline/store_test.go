package pipeline

import (
	"os"
	"testing"
)

func TestEnsureS3EnvFromMinIO(t *testing.T) {
	t.Setenv("AWS_ACCESS_KEY_ID", "")
	t.Setenv("AWS_SECRET_ACCESS_KEY", "")
	t.Setenv("MINIO_ACCESS_KEY", "minioadmin")
	t.Setenv("MINIO_SECRET_KEY", "minioadmin")

	ensureS3Env(StoreConfig{Backend: BackendMinIO})

	if os.Getenv("AWS_ACCESS_KEY_ID") != "minioadmin" {
		t.Fatalf("AWS_ACCESS_KEY_ID = %q", os.Getenv("AWS_ACCESS_KEY_ID"))
	}
	if os.Getenv("AWS_SECRET_ACCESS_KEY") != "minioadmin" {
		t.Fatalf("AWS_SECRET_ACCESS_KEY = %q", os.Getenv("AWS_SECRET_ACCESS_KEY"))
	}
}
