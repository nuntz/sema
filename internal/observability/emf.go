package observability

import (
	"encoding/json"
	"os"
	"sort"
	"sync"
	"time"
)

var outputMu sync.Mutex

// Emit writes a CloudWatch Embedded Metric Format event. The same event is
// still useful as structured JSON when EMF extraction is not enabled.
func Emit(metrics map[string]float64, dimensions map[string]string) {
	names := make([]string, 0, len(metrics))
	for name := range metrics {
		names = append(names, name)
	}
	sort.Strings(names)
	definitions := make([]map[string]string, 0, len(names))
	for _, name := range names {
		definitions = append(definitions, map[string]string{"Name": name, "Unit": unit(name)})
	}
	dimensionNames := make([]string, 0, len(dimensions))
	for name := range dimensions {
		dimensionNames = append(dimensionNames, name)
	}
	sort.Strings(dimensionNames)
	event := make(map[string]any, len(metrics)+len(dimensions)+1)
	event["_aws"] = map[string]any{
		"Timestamp":         time.Now().UnixMilli(),
		"CloudWatchMetrics": []any{map[string]any{"Namespace": "Sema", "Dimensions": [][]string{dimensionNames}, "Metrics": definitions}},
	}
	for name, value := range metrics {
		event[name] = value
	}
	for name, value := range dimensions {
		event[name] = value
	}
	outputMu.Lock()
	defer outputMu.Unlock()
	_ = json.NewEncoder(os.Stdout).Encode(event)
}

func unit(name string) string {
	if len(name) >= 2 && name[len(name)-2:] == "Ms" {
		return "Milliseconds"
	}
	return "Count"
}
