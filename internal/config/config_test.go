package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestSaveLoadRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "sub", "config.yaml")
	c := New(path)
	c.SetEnv("dev", Environment{Profile: "p", Region: "r"})
	c.HealthPatterns = []string{`GET /ping`}
	t1 := Target{Name: "svc", Env: "dev", Type: TypeEKS, Cluster: "c", Namespace: "n", Service: "svc"}
	if !c.AddTarget(t1) || c.AddTarget(t1) {
		t.Fatal("AddTarget did not de-duplicate")
	}
	if err := c.Save(); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("mode = %v", info.Mode().Perm())
	}
	got, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Targets) != 1 || got.Env("dev").Profile != "p" || got.RegionFor(got.Targets[0]) != "r" || len(got.HealthPatterns) != 1 {
		t.Fatalf("loaded = %+v", got)
	}
	if n := got.RemoveTargets([]string{got.Targets[0].Key()}); n != 1 || len(got.Targets) != 0 {
		t.Fatalf("remove = %d, %+v", n, got.Targets)
	}
}

func TestOverrides(t *testing.T) {
	c := New("x")
	c.SetEnv("prod", Environment{Profile: "env-profile", Region: "us-east-1"})
	tg := Target{Env: "prod", Type: TypeECS, Profile: "own-profile"}
	if c.ProfileFor(tg) != "own-profile" || c.RegionFor(tg) != "us-east-1" {
		t.Fatalf("profile=%q region=%q", c.ProfileFor(tg), c.RegionFor(tg))
	}
}

func TestLabels(t *testing.T) {
	cases := []struct {
		t       Target
		primary string
		label   string
	}{
		{Target{Type: TypeEKS, Cluster: "demo-cluster", Namespace: "default", Service: "orders-api"},
			"orders-api", "orders-api — EKS · demo-cluster / default [us-east-1]"},
		{Target{Type: TypeECS, Cluster: "arn:aws:ecs:us-east-1:123456789012:cluster/demo", Service: "arn:aws:ecs:us-east-1:123456789012:service/demo/billing-worker", LogGroup: "/ecs/billing-worker"},
			"billing-worker", "billing-worker — ECS · demo (/ecs/billing-worker) [us-east-1]"},
		{Target{Type: "lambda", Name: "fn"},
			"fn", "fn — lambda · unsupported target type [us-east-1]"},
	}
	for _, tc := range cases {
		if got := tc.t.Primary(); got != tc.primary {
			t.Errorf("Primary() = %q, want %q", got, tc.primary)
		}
		if got := tc.t.Label("us-east-1"); got != tc.label {
			t.Errorf("Label() = %q, want %q", got, tc.label)
		}
	}
}
