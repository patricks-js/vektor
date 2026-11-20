package metrics

import "testing"

func TestCatalogConsistencyAndLookup(t *testing.T) {
	all := AllMetrics()
	if len(all) == 0 {
		t.Fatal("expected non-empty metric catalog")
	}

	seen := make(map[string]struct{}, len(all))
	for _, def := range all {
		if _, dup := seen[def.Key]; dup {
			t.Fatalf("duplicate metric key in catalog: %q", def.Key)
		}
		seen[def.Key] = struct{}{}
		if def.Unit == "" {
			t.Fatalf("metric %q has empty unit", def.Key)
		}
		if len(def.ResourceKinds) == 0 {
			t.Fatalf("metric %q has no resource kinds", def.Key)
		}
		if len(def.Aggregators) == 0 {
			t.Fatalf("metric %q has no aggregators", def.Key)
		}
		got, ok := LookupMetric(def.Key)
		if !ok || got.Key != def.Key {
			t.Fatalf("LookupMetric(%q) failed", def.Key)
		}
	}

	for rawKey, rateKey := range CounterOutputKey {
		if !IsKnownMetric(rateKey) {
			t.Fatalf("counter output key %q (from %q) is missing from catalog", rateKey, rawKey)
		}
	}

	if IsKnownMetric("custom.arbitrary.metric") {
		t.Fatal("unexpected arbitrary metric allowed")
	}
}

func TestAlignBucketMs(t *testing.T) {
	cases := []struct {
		tsMs  int64
		resMs int64
		want  int64
	}{
		{0, 30000, 0},
		{29999, 30000, 0},
		{30000, 30000, 30000},
		{45123, 30000, 30000},
		{3599999, 3600000, 0},
		{3600000, 3600000, 3600000},
		{-1, 30000, -30000},
	}
	for _, tc := range cases {
		if got := AlignBucketMs(tc.tsMs, tc.resMs); got != tc.want {
			t.Fatalf("AlignBucketMs(%d, %d) = %d, want %d", tc.tsMs, tc.resMs, got, tc.want)
		}
	}
}
