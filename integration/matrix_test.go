//go:build integration

package integration

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/codewandler/llm"
)

type matrixResultEntry struct {
	Target      string    `json:"target"`
	Scenario    string    `json:"scenario"`
	Status      string    `json:"status"`
	Reason      string    `json:"reason,omitempty"`
	Model       string    `json:"model,omitempty"`
	ServiceID   string    `json:"service_id,omitempty"`
	Provider    string    `json:"provider,omitempty"`
	APIType     string    `json:"api_type,omitempty"`
	CompletedAt time.Time `json:"completed_at"`
}

type matrixResultReport struct {
	GeneratedAt time.Time           `json:"generated_at"`
	Entries     []matrixResultEntry `json:"entries"`
}

var matrixResults struct {
	sync.Mutex
	entries []matrixResultEntry
}

func recordMatrixResult(entry matrixResultEntry) {
	matrixResults.Lock()
	defer matrixResults.Unlock()
	matrixResults.entries = append(matrixResults.entries, entry)
}

func snapshotMatrixResults() []matrixResultEntry {
	matrixResults.Lock()
	defer matrixResults.Unlock()
	out := append([]matrixResultEntry(nil), matrixResults.entries...)
	sort.Slice(out, func(i, j int) bool {
		if out[i].Target != out[j].Target {
			return out[i].Target < out[j].Target
		}
		return out[i].Scenario < out[j].Scenario
	})
	return out
}

func writeMatrixReports(t *testing.T) {
	t.Helper()
	entries := snapshotMatrixResults()
	report := matrixResultReport{GeneratedAt: time.Now(), Entries: entries}
	if path := os.Getenv("MATRIX_RESULTS_JSON"); path != "" {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatalf("mkdir for MATRIX_RESULTS_JSON: %v", err)
		}
		data, err := json.MarshalIndent(report, "", "  ")
		if err != nil {
			t.Fatalf("marshal matrix report: %v", err)
		}
		if err := os.WriteFile(path, data, 0o644); err != nil {
			t.Fatalf("write MATRIX_RESULTS_JSON: %v", err)
		}
	}
	if path := os.Getenv("MATRIX_RESULTS_MD"); path != "" {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatalf("mkdir for MATRIX_RESULTS_MD: %v", err)
		}
		var b strings.Builder
		fmt.Fprintf(&b, "# Integration Matrix Results\n\n")
		fmt.Fprintf(&b, "Generated: %s\n\n", report.GeneratedAt.Format(time.RFC3339))
		fmt.Fprintf(&b, "| Target | Scenario | Status | Service | Provider | API | Reason |\n")
		fmt.Fprintf(&b, "|---|---|---|---|---|---|---|\n")
		for _, e := range entries {
			fmt.Fprintf(&b, "| %s | %s | %s | %s | %s | %s | %s |\n",
				e.Target,
				e.Scenario,
				statusEmoji(e.Status),
				emptyDash(e.ServiceID),
				emptyDash(e.Provider),
				emptyDash(e.APIType),
				sanitizeCell(e.Reason),
			)
		}
		if err := os.WriteFile(path, []byte(b.String()), 0o644); err != nil {
			t.Fatalf("write MATRIX_RESULTS_MD: %v", err)
		}
	}
}

func emptyDash(v string) string {
	if v == "" {
		return "-"
	}
	return sanitizeCell(v)
}

func sanitizeCell(v string) string {
	v = strings.ReplaceAll(v, "\n", " ")
	v = strings.ReplaceAll(v, "|", "\\|")
	if v == "" {
		return "-"
	}
	return v
}

func statusEmoji(status string) string {
	switch status {
	case "pass":
		return "✅"
	case "fail":
		return "❌"
	case "skip":
		return "⏭️"
	default:
		return "❓"
	}
}

func TestIntegrationMatrix(t *testing.T) {
	if os.Getenv("RUN_INTEGRATION") != "1" {
		t.Skip("set RUN_INTEGRATION=1 to run integration tests")
	}
	defer writeMatrixReports(t)

	targets := integrationTargets()
	scenarios := integrationScenarios()

	for _, target := range targets {
		target := target
		t.Run(target.name, func(t *testing.T) {
			if ok, reason := target.available(); !ok {
				recordMatrixResult(matrixResultEntry{Target: target.name, Status: "skip", Reason: reason, Model: target.model, CompletedAt: time.Now()})
				t.Skip(reason)
			}
			for _, scenario := range scenarios {
				scenario := scenario
				t.Run(scenario.name, func(t *testing.T) {
					entry := matrixResultEntry{Target: target.name, Scenario: scenario.name, Model: target.model, CompletedAt: time.Now()}
					defer func() {
						entry.CompletedAt = time.Now()
						if entry.Status == "" {
							switch {
							case t.Skipped():
								entry.Status = "skip"
							case t.Failed():
								entry.Status = "fail"
							default:
								entry.Status = "pass"
							}
						}
						recordMatrixResult(entry)
					}()
					if scenario.enabled != nil {
						if ok, reason := scenario.enabled(target); !ok {
							entry.Status = "skip"
							entry.Reason = reason
							t.Skip(reason)
						}
					}
					run := executeIntegrationScenario(t, target, scenario)
					entry.ServiceID = firstCandidateServiceID(run.candidates)
					entry.Provider = firstCandidateName(run.candidates)
					if run.requestEvent != nil {
						entry.APIType = string(run.requestEvent.ResolvedApiType)
					}
					scenario.assert(t, run)
				})
			}
		})
	}
}

func firstCandidateServiceID(candidates []llm.RegisteredProvider) string {
	if len(candidates) == 0 {
		return ""
	}
	return candidates[0].ServiceID
}
