package client

import (
	"context"
	"fmt"

	"go.temporal.io/sdk/client"
)

// HealthCheck performs a liveness probe against the Temporal server.
// Returns nil if the server is reachable, otherwise returns an error.
//
// Suitable for Kubernetes readiness probes:
//
//	if err := engine.HealthCheck(ctx); err != nil {
//	    http.Error(w, "not ready", http.StatusServiceUnavailable)
//	}
func (e *Engine) HealthCheck(ctx context.Context) error {
	_, err := e.client.CheckHealth(ctx, &client.CheckHealthRequest{})
	if err != nil {
		return fmt.Errorf("temporal health check failed: %w", err)
	}
	return nil
}
