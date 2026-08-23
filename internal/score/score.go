package score

import (
	"encoding/binary"
	"math"
	"sort"
	"strings"
	"time"

	"github.com/nuntz/sema/internal/domain"
)

const (
	VersionV2              = "2"
	ImplicitOpen           = 0.2
	ImplicitDwell          = 0.2
	ImplicitClick          = 0.3
	DwellThreshold         = 30_000
	PriorWindow            = 90 * 24 * time.Hour
	LegacyEmbeddingVersion = "amazon.titan-embed-text-v2:0"
)

type Result struct {
	Score float64
	Base  float64
	Taste float64
	Prior float64
}

type Candidate struct {
	Title     string
	FeedTitle string
	Vector    []float32
}

// Calculate implements scoring v2. All stored vectors are normalized, but Dot
// deliberately tolerates old unnormalized rows during the first model rebuild.
func Calculate(vector []float32, model domain.Model, feedID string, hasMedia bool, ageHours float64) Result {
	base, taste := 0.5, 0.0
	if model.ExplicitCount >= 10 {
		simLiked, simDisliked := 0.0, 0.0
		if model.LikedCount >= 5 {
			simLiked = Dot(vector, DecodeVector(model.LikedCentroid))
		}
		if model.DislikedCount >= 5 {
			simDisliked = Dot(vector, DecodeVector(model.DislikedCentroid))
		}
		taste = simLiked - simDisliked
		base = clamp(0.5+0.8*taste, 0, 1)
	}
	prior := model.FeedPrior[feedID]
	mediaBonus := 0.0
	if hasMedia {
		mediaBonus = 0.05
	}
	recency := math.Exp(-math.Max(0, ageHours) / 48)
	value := clamp(base+prior+mediaBonus, 0, 1) * (0.7 + 0.3*recency)
	return Result{Score: value, Base: base, Taste: taste, Prior: prior}
}

// LegacyCalculate remains available for a controlled scoring-version rollback.
func LegacyCalculate(vector []float32, signals []Signal, hasMedia bool, published, fetched time.Time) float64 {
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

// Why selects the dominant positive explanation. Prior-driven explanations do
// not falsely attribute a score to a merely similar item.
func Why(result Result, vector []float32, feedTitle string, candidates []Candidate) *domain.Why {
	if result.Base <= 0.6 {
		return nil
	}
	if result.Prior > math.Max(0, 0.8*result.Taste) && feedTitle != "" {
		return &domain.Why{FeedTitle: truncate(feedTitle, 80)}
	}
	best := -math.MaxFloat64
	var selected Candidate
	for _, candidate := range candidates {
		value := Dot(vector, candidate.Vector)
		if value > best {
			best = value
			selected = candidate
		}
	}
	if selected.Title == "" {
		return nil
	}
	return &domain.Why{Title: truncate(selected.Title, 80), FeedTitle: truncate(selected.FeedTitle, 80)}
}

func BuildModel(userID string, signals []domain.Signal, behaviours []domain.Behaviour, now time.Time, version string) domain.Model {
	model := domain.Model{
		PK: domain.UserPK(userID), SK: "MODEL", Version: version, ComputedAt: domain.Timestamp(now),
		FeedPrior: make(map[string]float64), FeedSignalCount: make(map[string]int),
		FeedLikes: make(map[string]int), FeedDislikes: make(map[string]int), FeedImplicit: make(map[string]int),
	}
	explicit := make(map[string]bool, len(signals))
	var likedSum, dislikedSum []float32
	cutoff := now.Add(-PriorWindow)
	for _, signal := range signals {
		model.ExplicitCount++
		explicit[signal.ItemID] = true
		if signal.Value > 0 {
			model.LikedCount++
		} else if signal.Value < 0 {
			model.DislikedCount++
		}
		if CompatibleVersion(signal.ModelVersion, version) {
			vector := Normalize(DecodeVector(signal.Vector))
			if signal.Value > 0 {
				likedSum = addWeighted(likedSum, vector, 1)
				model.LikedWeight++
			} else if signal.Value < 0 {
				dislikedSum = addWeighted(dislikedSum, vector, 1)
				model.DislikedWeight++
			}
		}
		if within(signal.CreatedAt, cutoff) {
			if signal.Value > 0 {
				model.FeedLikes[signal.FeedID]++
			} else if signal.Value < 0 {
				model.FeedDislikes[signal.FeedID]++
			}
		}
	}
	for _, behaviour := range behaviours {
		if !within(behaviour.OpenedAt, cutoff) {
			continue
		}
		model.ImplicitCount++
		model.FeedImplicit[behaviour.FeedID]++
		if explicit[behaviour.ItemID] || !CompatibleVersion(behaviour.ModelVersion, version) {
			continue
		}
		weight := BehaviourWeight(behaviour)
		if weight == 0 {
			continue
		}
		likedSum = addWeighted(likedSum, Normalize(DecodeVector(behaviour.Vector)), weight)
		model.LikedWeight += weight
	}
	model.LikedSum = EncodeVector(likedSum)
	model.DislikedSum = EncodeVector(dislikedSum)
	model.LikedCentroid = EncodeVector(Normalize(likedSum))
	model.DislikedCentroid = EncodeVector(Normalize(dislikedSum))
	recomputePriors(&model)
	return model
}

func BehaviourWeight(row domain.Behaviour) float64 {
	weight := 0.0
	if row.Opened {
		weight += ImplicitOpen
	}
	if row.DwellMS >= DwellThreshold {
		weight += ImplicitDwell
	}
	if row.ClickedThrough || row.Shared {
		weight += ImplicitClick
	}
	return weight
}

// ApplyExplicit updates a fully materialized model in place. It also removes
// or restores the implicit vector for the same item so explicit always wins.
func ApplyExplicit(model *domain.Model, oldSignal, newSignal *domain.Signal, behaviour *domain.Behaviour, now time.Time) bool {
	if model == nil || model.FeedLikes == nil || model.FeedDislikes == nil || model.FeedImplicit == nil {
		return false
	}
	if model.ExplicitCount != model.LikedCount+model.DislikedCount {
		return false
	}
	likedSum := DecodeVector(model.LikedSum)
	dislikedSum := DecodeVector(model.DislikedSum)
	if (model.LikedWeight > 0 && len(likedSum) == 0) || (model.DislikedWeight > 0 && len(dislikedSum) == 0) {
		return false
	}
	cutoff := now.Add(-PriorWindow)
	apply := func(signal *domain.Signal, direction float64) {
		if signal == nil {
			return
		}
		vector := Normalize(DecodeVector(signal.Vector))
		if signal.Value > 0 {
			likedSum = addWeighted(likedSum, vector, direction)
			model.LikedWeight += direction
			model.LikedCount += int(direction)
		} else if signal.Value < 0 {
			dislikedSum = addWeighted(dislikedSum, vector, direction)
			model.DislikedWeight += direction
			model.DislikedCount += int(direction)
		}
		model.ExplicitCount += int(direction)
		if within(signal.CreatedAt, cutoff) {
			if signal.Value > 0 {
				model.FeedLikes[signal.FeedID] += int(direction)
			} else if signal.Value < 0 {
				model.FeedDislikes[signal.FeedID] += int(direction)
			}
		}
	}
	apply(oldSignal, -1)
	apply(newSignal, 1)
	if behaviour != nil && within(behaviour.OpenedAt, now.Add(-PriorWindow)) && CompatibleVersion(behaviour.ModelVersion, model.Version) {
		weight := BehaviourWeight(*behaviour)
		if oldSignal == nil && newSignal != nil {
			likedSum = addWeighted(likedSum, Normalize(DecodeVector(behaviour.Vector)), -weight)
			model.LikedWeight -= weight
		} else if oldSignal != nil && newSignal == nil {
			likedSum = addWeighted(likedSum, Normalize(DecodeVector(behaviour.Vector)), weight)
			model.LikedWeight += weight
		}
	}
	if model.LikedWeight < 1e-9 {
		model.LikedWeight, likedSum = 0, nil
	}
	if model.DislikedWeight < 1e-9 {
		model.DislikedWeight, dislikedSum = 0, nil
	}
	model.LikedSum = EncodeVector(likedSum)
	model.DislikedSum = EncodeVector(dislikedSum)
	model.LikedCentroid = EncodeVector(Normalize(likedSum))
	model.DislikedCentroid = EncodeVector(Normalize(dislikedSum))
	model.ComputedAt = domain.Timestamp(now)
	recomputePriors(model)
	return true
}

func recomputePriors(model *domain.Model) {
	model.FeedPrior = make(map[string]float64)
	model.FeedSignalCount = make(map[string]int)
	feeds := make(map[string]bool)
	for id := range model.FeedLikes {
		feeds[id] = true
	}
	for id := range model.FeedDislikes {
		feeds[id] = true
	}
	for id := range model.FeedImplicit {
		feeds[id] = true
	}
	for id := range feeds {
		likes, dislikes, implicit := model.FeedLikes[id], model.FeedDislikes[id], model.FeedImplicit[id]
		model.FeedPrior[id] = 0.15 * math.Tanh((float64(likes-dislikes)+0.3*float64(implicit))/5)
		model.FeedSignalCount[id] = likes + dislikes + implicit
	}
}

func Size(value float64, model domain.Model) string {
	p60, p90 := 0.45, 0.75
	if model.ExplicitCount >= 10 && model.SizeCutoffs != nil {
		p60, p90 = model.SizeCutoffs.P60, model.SizeCutoffs.P90
	}
	if value >= p90 {
		return "L"
	}
	if value >= p60 {
		return "M"
	}
	return "S"
}

// QuantileCutoffs uses linearly interpolated sample quantiles. Interpolation
// keeps a unique ten-item distribution at one item (10%) in the large bucket.
func QuantileCutoffs(values []float64) *domain.SizeCutoffs {
	if len(values) == 0 {
		return nil
	}
	sorted := append([]float64(nil), values...)
	sort.Float64s(sorted)
	return &domain.SizeCutoffs{P60: quantile(sorted, 0.60), P90: quantile(sorted, 0.90)}
}

func quantile(sorted []float64, percentile float64) float64 {
	position := percentile * float64(len(sorted)-1)
	lower := int(math.Floor(position))
	upper := int(math.Ceil(position))
	if lower == upper {
		return sorted[lower]
	}
	weight := position - float64(lower)
	return sorted[lower] + weight*(sorted[upper]-sorted[lower])
}

func Dot(a, b []float32) float64 {
	if len(a) == 0 || len(a) != len(b) {
		return 0
	}
	var dot float64
	for i := range a {
		dot += float64(a[i]) * float64(b[i])
	}
	return dot
}

func Cosine(a, b []float32) float64 { return Dot(Normalize(a), Normalize(b)) }

func Normalize(values []float32) []float32 {
	if len(values) == 0 {
		return nil
	}
	var norm float64
	for _, value := range values {
		norm += float64(value) * float64(value)
	}
	if norm == 0 {
		return make([]float32, len(values))
	}
	result := make([]float32, len(values))
	scale := float32(1 / math.Sqrt(norm))
	for i, value := range values {
		result[i] = value * scale
	}
	return result
}

func EncodeVector(values []float32) []byte {
	if len(values) == 0 {
		return nil
	}
	encoded := make([]byte, len(values)*4)
	for i, value := range values {
		binary.LittleEndian.PutUint32(encoded[i*4:], math.Float32bits(value))
	}
	return encoded
}

func DecodeVector(encoded []byte) []float32 {
	if len(encoded)%4 != 0 {
		return nil
	}
	values := make([]float32, len(encoded)/4)
	for i := range values {
		values[i] = math.Float32frombits(binary.LittleEndian.Uint32(encoded[i*4:]))
	}
	return values
}

func addWeighted(sum, vector []float32, weight float64) []float32 {
	if len(vector) == 0 {
		return sum
	}
	if len(sum) == 0 {
		sum = make([]float32, len(vector))
	}
	if len(sum) != len(vector) {
		return sum
	}
	for i := range sum {
		sum[i] += float32(weight) * vector[i]
	}
	return sum
}

func CompatibleVersion(row, target string) bool {
	return row == target || target == "" || (row == "" && target == LegacyEmbeddingVersion)
}

func within(value string, cutoff time.Time) bool {
	parsed, err := time.Parse(time.RFC3339Nano, value)
	return err == nil && !parsed.Before(cutoff)
}

func truncate(value string, count int) string {
	value = strings.TrimSpace(value)
	runes := []rune(value)
	if len(runes) <= count {
		return value
	}
	return string(runes[:count])
}

func clamp(value, low, high float64) float64 { return math.Min(high, math.Max(low, value)) }
