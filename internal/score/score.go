package score

import (
	"encoding/binary"
	"math"
	"time"
)

type Signal struct {
	Value  int
	Vector []float32
}

func Calculate(vector []float32, signals []Signal, hasMedia bool, published, fetched time.Time) float64 {
	base := 0.5
	if len(signals) >= 10 {
		liked, likedN := 0.0, 0
		disliked, dislikedN := 0.0, 0
		for _, signal := range signals {
			similarity := Cosine(vector, signal.Vector)
			if signal.Value > 0 {
				liked += similarity
				likedN++
			} else if signal.Value < 0 {
				disliked += similarity
				dislikedN++
			}
		}
		if likedN > 0 {
			liked /= float64(likedN)
		}
		if dislikedN > 0 {
			disliked /= float64(dislikedN)
		}
		base = clamp(0.5+liked-disliked, 0, 1)
	}
	if hasMedia {
		base += 0.05
	}
	ageHours := math.Max(0, fetched.Sub(published).Hours())
	recency := math.Exp(-ageHours / 48)
	return clamp(base, 0, 1) * (0.7 + 0.3*recency)
}

func Size(value float64) string {
	if value >= 0.75 {
		return "L"
	}
	if value >= 0.45 {
		return "M"
	}
	return "S"
}

func Cosine(a, b []float32) float64 {
	if len(a) == 0 || len(a) != len(b) {
		return 0
	}
	var dot, normA, normB float64
	for i := range a {
		x, y := float64(a[i]), float64(b[i])
		dot += x * y
		normA += x * x
		normB += y * y
	}
	if normA == 0 || normB == 0 {
		return 0
	}
	return dot / (math.Sqrt(normA) * math.Sqrt(normB))
}

func EncodeVector(values []float32) []byte {
	encoded := make([]byte, len(values)*4)
	for i, value := range values {
		binary.LittleEndian.PutUint32(encoded[i*4:], math.Float32bits(value))
	}
	return encoded
}

func DecodeVector(encoded []byte) []float32 {
	values := make([]float32, len(encoded)/4)
	for i := range values {
		values[i] = math.Float32frombits(binary.LittleEndian.Uint32(encoded[i*4:]))
	}
	return values
}

func clamp(value, low, high float64) float64 {
	return math.Min(high, math.Max(low, value))
}
