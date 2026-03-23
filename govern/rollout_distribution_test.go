package govern

import (
	"fmt"
	"math"
	"testing"
)

func TestRolloutBucketDistribution(t *testing.T) {
	const n = 100_000
	const namespace = "test:distribution"

	for _, pct := range []float64{1, 5, 10, 25, 50, 75, 90, 99} {
		t.Run(fmt.Sprintf("%g_pct", pct), func(t *testing.T) {
			threshold := uint16(pct * float64(RolloutResolution) / 100)
			allowed := 0
			for i := range n {
				subject := fmt.Sprintf("user-%d", i)
				bucket := RolloutBucket(namespace, subject)
				if bucket < threshold {
					allowed++
				}
			}

			observed := float64(allowed) / float64(n) * 100
			tolerance := 1.0 // 1 percentage point
			if math.Abs(observed-pct) > tolerance {
				t.Errorf("rollout %g%%: expected ~%g%% allowed, got %.2f%% (%d/%d)",
					pct, pct, observed, allowed, n)
			}
		})
	}
}

func TestRolloutBucketUniformity(t *testing.T) {
	// Verify buckets are roughly uniformly distributed across the full range.
	const n = 100_000
	const namespace = "test:uniformity"
	const numBins = 100

	bins := make([]int, numBins)
	for i := range n {
		subject := fmt.Sprintf("subject-%d", i)
		bucket := RolloutBucket(namespace, subject)
		bin := int(bucket) * numBins / int(RolloutResolution)
		if bin >= numBins {
			bin = numBins - 1
		}
		bins[bin]++
	}

	expected := float64(n) / float64(numBins)
	// Chi-squared test at p=0.001 for 99 degrees of freedom: critical value ~148.
	var chiSq float64
	for _, count := range bins {
		diff := float64(count) - expected
		chiSq += (diff * diff) / expected
	}

	if chiSq > 148.0 {
		t.Errorf("bucket distribution fails chi-squared test: χ²=%.1f (critical=148.0 at p=0.001, df=99)", chiSq)
	}
}

func TestRolloutBucketNamespaceIndependence(t *testing.T) {
	// Same subject, different namespaces should produce different buckets.
	const subject = "user-12345"
	b1 := RolloutBucket("namespace-a", subject)
	b2 := RolloutBucket("namespace-b", subject)
	if b1 == b2 {
		t.Errorf("same subject in different namespaces got identical bucket %d — expected independence", b1)
	}
}

func TestRolloutBucketDeterminism(t *testing.T) {
	const namespace = "test:determinism"
	const subject = "user-42"
	first := RolloutBucket(namespace, subject)
	for range 1000 {
		if got := RolloutBucket(namespace, subject); got != first {
			t.Fatalf("non-deterministic: first=%d got=%d", first, got)
		}
	}
}
