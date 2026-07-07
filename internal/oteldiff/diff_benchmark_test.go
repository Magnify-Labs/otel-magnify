package oteldiff

import (
	"os"
	"testing"
)

func loadBenchmarkFixture(b *testing.B, name string) []byte {
	b.Helper()
	data, err := os.ReadFile("testdata/" + name)
	if err != nil {
		b.Fatal(err)
	}
	return data
}

func BenchmarkCompareHighRiskConfig(b *testing.B) {
	base := loadBenchmarkFixture(b, "base.yaml")
	target := loadBenchmarkFixture(b, "high-target.yaml")

	b.ReportAllocs()
	for b.Loop() {
		got := Compare(base, target)
		if !got.Valid {
			b.Fatalf("expected valid diff, diagnostics=%v", got.Diagnostics)
		}
	}
}

func BenchmarkCompareWithBlastRadiusContext(b *testing.B) {
	base := loadBenchmarkFixture(b, "base.yaml")
	target := loadBenchmarkFixture(b, "high-target.yaml")
	ctx := BlastRadiusContext{
		Workload: BlastRadiusWorkload{
			ID:          "collector-prod-eu",
			DisplayName: "collector-prod-eu",
			Type:        "collector",
			Status:      "connected",
			Labels: map[string]string{
				"service.name":     "checkout",
				"k8s.cluster.name": "prod-eu",
				"tier":             "critical",
			},
		},
		FleetPeers: []BlastRadiusWorkload{
			{
				ID:          "collector-prod-eu-peer",
				DisplayName: "collector-prod-eu-peer",
				Type:        "collector",
				Status:      "connected",
				Labels: map[string]string{
					"service.name":     "billing",
					"k8s.cluster.name": "prod-eu",
				},
			},
		},
	}

	b.ReportAllocs()
	for b.Loop() {
		got := CompareWithContext(base, target, ctx)
		if !got.Valid {
			b.Fatalf("expected valid diff, diagnostics=%v", got.Diagnostics)
		}
	}
}
