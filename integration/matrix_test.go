//go:build integration

package integration

import (
	"os"
	"testing"
)

func TestIntegrationMatrix(t *testing.T) {
	if os.Getenv("RUN_INTEGRATION") != "1" {
		t.Skip("set RUN_INTEGRATION=1 to run integration tests")
	}

	providers := matrixProviders()
	scenarios := matrixScenarios()

	for _, provider := range providers {
		provider := provider
		t.Run(provider.name, func(t *testing.T) {
			if ok, reason := provider.available(); !ok {
				t.Skip(reason)
			}

			for _, scenario := range scenarios {
				scenario := scenario
				t.Run(scenario.name, func(t *testing.T) {
					run := executeMatrixScenario(t, provider, scenario)
					scenario.assert(t, run)
				})
			}
		})
	}
}
