package memory

import (
	"bytes"
	"encoding/binary"
	"math"
	"sort"
)

// CosineSimilarity calculates the cosine similarity between two float32 vectors.
// Returns a value between -1 and 1, where 1 means identical direction.
func CosineSimilarity(a, b []float32) float32 {
	if len(a) != len(b) || len(a) == 0 {
		return 0
	}

	var dotProduct, normA, normB float32
	for i := range a {
		dotProduct += a[i] * b[i]
		normA += a[i] * a[i]
		normB += b[i] * b[i]
	}

	if normA == 0 || normB == 0 {
		return 0
	}

	return dotProduct / (float32(math.Sqrt(float64(normA))) * float32(math.Sqrt(float64(normB))))
}

// Float32ArrayToBytes converts a float32 slice to a byte slice for SQLite BLOB storage.
func Float32ArrayToBytes(floats []float32) ([]byte, error) {
	buf := new(bytes.Buffer)
	err := binary.Write(buf, binary.LittleEndian, floats)
	if err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// BytesToFloat32Array converts a byte slice back to a float32 slice.
func BytesToFloat32Array(b []byte) ([]float32, error) {
	if len(b) == 0 {
		return nil, nil
	}
	floats := make([]float32, len(b)/4)
	buf := bytes.NewReader(b)
	err := binary.Read(buf, binary.LittleEndian, &floats)
	if err != nil {
		return nil, err
	}
	return floats, nil
}

// ScoredFinding wraps a Finding with its similarity score for sorting.
type ScoredFinding struct {
	Finding *Finding
	Score   float32
}

// SortFindingsBySimilarity calculates similarity against a query vector and returns the top N results.
func SortFindingsBySimilarity(queryVector []float32, findings []*Finding, limit int) []*Finding {
	if len(findings) == 0 || len(queryVector) == 0 {
		return findings
	}

	scored := make([]ScoredFinding, 0, len(findings))
	for _, f := range findings {
		if len(f.Embedding) > 0 {
			score := CosineSimilarity(queryVector, f.Embedding)
			scored = append(scored, ScoredFinding{Finding: f, Score: score})
		}
	}

	// Sort descending by score
	sort.Slice(scored, func(i, j int) bool {
		return scored[i].Score > scored[j].Score
	})

	if limit > 0 && limit < len(scored) {
		scored = scored[:limit]
	}

	result := make([]*Finding, len(scored))
	for i, s := range scored {
		result[i] = s.Finding
	}
	
	return result
}
