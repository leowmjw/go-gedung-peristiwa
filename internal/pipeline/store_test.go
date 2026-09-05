package pipeline

import (
	"os"
	"testing"
	"time"
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

func TestNormalizeChangeFeedRetainFor(t *testing.T) {
	cases := []struct {
		name string
		in   time.Duration
		want time.Duration
	}{
		{"zero defaults to 30 days", 0, DefaultChangeFeedRetainFor},
		{"negative defaults to 30 days", -time.Hour, DefaultChangeFeedRetainFor},
		{"within bounds passes through", 60 * 24 * time.Hour, 60 * 24 * time.Hour},
		{"above one year clamps", 2 * MaxChangeFeedRetainFor, MaxChangeFeedRetainFor},
		{"exactly one year passes through", MaxChangeFeedRetainFor, MaxChangeFeedRetainFor},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := NormalizeChangeFeedRetainFor(tc.in); got != tc.want {
				t.Fatalf("NormalizeChangeFeedRetainFor(%v) = %v, want %v", tc.in, got, tc.want)
			}
		})
	}
}

func TestChangeFeedRetainForFromEnv(t *testing.T) {
	t.Run("unset defaults to 30 days", func(t *testing.T) {
		t.Setenv("CHANGEFEED_RETAIN_FOR", "")
		if got := changeFeedRetainForFromEnv("CHANGEFEED_RETAIN_FOR"); got != DefaultChangeFeedRetainFor {
			t.Fatalf("got %v, want %v", got, DefaultChangeFeedRetainFor)
		}
	})
	t.Run("invalid falls back to default", func(t *testing.T) {
		t.Setenv("CHANGEFEED_RETAIN_FOR", "not-a-duration")
		if got := changeFeedRetainForFromEnv("CHANGEFEED_RETAIN_FOR"); got != DefaultChangeFeedRetainFor {
			t.Fatalf("got %v, want %v", got, DefaultChangeFeedRetainFor)
		}
	})
	t.Run("valid duration is clamped and honored", func(t *testing.T) {
		t.Setenv("CHANGEFEED_RETAIN_FOR", "8760h")
		if got := changeFeedRetainForFromEnv("CHANGEFEED_RETAIN_FOR"); got != MaxChangeFeedRetainFor {
			t.Fatalf("got %v, want %v", got, MaxChangeFeedRetainFor)
		}
	})
	t.Run("over max is clamped", func(t *testing.T) {
		t.Setenv("CHANGEFEED_RETAIN_FOR", "20000h")
		if got := changeFeedRetainForFromEnv("CHANGEFEED_RETAIN_FOR"); got != MaxChangeFeedRetainFor {
			t.Fatalf("got %v, want %v", got, MaxChangeFeedRetainFor)
		}
	})
}

func TestStoreConfigFromEnvChangeFeedRetainFor(t *testing.T) {
	t.Setenv("CHANGEFEED_RETAIN_FOR", "48h")
	cfg := StoreConfigFromEnv(BackendMinIO, "")
	if cfg.ChangeFeedRetainFor != 48*time.Hour {
		t.Fatalf("ChangeFeedRetainFor = %v, want 48h", cfg.ChangeFeedRetainFor)
	}
}
