package memory

import (
	"math"
	"testing"
)

func TestCosineSimilarity_IdenticalVectors(t *testing.T) {
	a := []float32{1.0, 2.0, 3.0}
	b := []float32{1.0, 2.0, 3.0}
	score := CosineSimilarity(a, b)
	if math.Abs(float64(score)-1.0) > 1e-6 {
		t.Errorf("identical vectors should have similarity 1.0, got %f", score)
	}
}

func TestCosineSimilarity_OrthogonalVectors(t *testing.T) {
	a := []float32{1.0, 0.0}
	b := []float32{0.0, 1.0}
	score := CosineSimilarity(a, b)
	if math.Abs(float64(score)) > 1e-6 {
		t.Errorf("orthogonal vectors should have similarity 0.0, got %f", score)
	}
}

func TestCosineSimilarity_OppositeVectors(t *testing.T) {
	a := []float32{1.0, 2.0, 3.0}
	b := []float32{-1.0, -2.0, -3.0}
	score := CosineSimilarity(a, b)
	if math.Abs(float64(score)+1.0) > 1e-6 {
		t.Errorf("opposite vectors should have similarity -1.0, got %f", score)
	}
}

func TestCosineSimilarity_EmptyVectors(t *testing.T) {
	score := CosineSimilarity(nil, nil)
	if score != 0 {
		t.Errorf("empty vectors should return 0, got %f", score)
	}
}

func TestCosineSimilarity_DifferentLengths(t *testing.T) {
	a := []float32{1.0, 2.0}
	b := []float32{1.0, 2.0, 3.0}
	score := CosineSimilarity(a, b)
	if score != 0 {
		t.Errorf("mismatched lengths should return 0, got %f", score)
	}
}

func TestFloat32RoundTrip(t *testing.T) {
	original := []float32{0.12, -0.45, 0.89, 1.23, -0.001}
	b, err := Float32ArrayToBytes(original)
	if err != nil {
		t.Fatalf("Float32ArrayToBytes: %v", err)
	}

	restored, err := BytesToFloat32Array(b)
	if err != nil {
		t.Fatalf("BytesToFloat32Array: %v", err)
	}

	if len(restored) != len(original) {
		t.Fatalf("length mismatch: got %d, want %d", len(restored), len(original))
	}
	for i := range original {
		if math.Abs(float64(original[i]-restored[i])) > 1e-7 {
			t.Errorf("index %d: got %f, want %f", i, restored[i], original[i])
		}
	}
}

func TestBytesToFloat32Array_Empty(t *testing.T) {
	result, err := BytesToFloat32Array(nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result != nil {
		t.Errorf("expected nil, got %v", result)
	}
}

func TestSortFindingsBySimilarity(t *testing.T) {
	queryVector := []float32{1.0, 0.0, 0.0}

	findings := []*Finding{
		{Key: "orthogonal", Embedding: []float32{0.0, 1.0, 0.0}},    // score ~0
		{Key: "close-match", Embedding: []float32{0.9, 0.1, 0.05}},   // score ~0.99
		{Key: "exact-match", Embedding: []float32{1.0, 0.0, 0.0}},    // score = 1.0
		{Key: "no-embedding"},                                         // skipped
	}

	sorted := SortFindingsBySimilarity(queryVector, findings, 3)

	if len(sorted) != 3 {
		t.Fatalf("expected 3 results, got %d", len(sorted))
	}
	if sorted[0].Key != "exact-match" {
		t.Errorf("expected first result to be 'exact-match', got %q", sorted[0].Key)
	}
	if sorted[1].Key != "close-match" {
		t.Errorf("expected second result to be 'close-match', got %q", sorted[1].Key)
	}
	if sorted[2].Key != "orthogonal" {
		t.Errorf("expected third result to be 'orthogonal', got %q", sorted[2].Key)
	}
}

func TestSortFindingsBySimilarity_WithLimit(t *testing.T) {
	queryVector := []float32{1.0, 0.0}
	findings := []*Finding{
		{Key: "a", Embedding: []float32{0.5, 0.5}},
		{Key: "b", Embedding: []float32{0.9, 0.1}},
		{Key: "c", Embedding: []float32{0.1, 0.9}},
	}

	sorted := SortFindingsBySimilarity(queryVector, findings, 1)
	if len(sorted) != 1 {
		t.Fatalf("expected 1 result with limit, got %d", len(sorted))
	}
	if sorted[0].Key != "b" {
		t.Errorf("expected top result to be 'b', got %q", sorted[0].Key)
	}
}
