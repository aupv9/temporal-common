package client

import (
	"context"
	"fmt"
	"reflect"
	"runtime"
	"strings"
	"time"

	filterpb "go.temporal.io/api/filter/v1"
	workflowservice "go.temporal.io/api/workflowservice/v1"
	"go.uber.org/zap"
)

// SLABreachEvent is delivered to SLAConfig.AlertFn when a workflow exceeds
// the configured alert threshold.
type SLABreachEvent struct {
	// WorkflowID is the ID of the breaching workflow.
	WorkflowID string

	// WorkflowType is the registered name of the workflow function.
	WorkflowType string

	// RunningFor is how long the workflow has been running at the time of the alert.
	RunningFor time.Duration

	// Threshold is the SLA threshold that was breached (WarnAfter or AlertAfter).
	Threshold time.Duration

	// Level is "warn" or "alert".
	Level string
}

// SLAConfig configures SLA monitoring for a workflow type.
type SLAConfig struct {
	// WarnAfter emits a warning log when a workflow has been running longer than
	// this duration. Zero = disabled.
	WarnAfter time.Duration

	// AlertAfter calls AlertFn when a workflow has been running longer than
	// this duration. Must be >= WarnAfter. Zero = disabled.
	AlertAfter time.Duration

	// AlertFn is called once per workflow when it breaches the AlertAfter threshold.
	// Runs in a background goroutine — may block or call external services.
	// If nil, only logging occurs.
	AlertFn func(ctx context.Context, event SLABreachEvent)
}

// slaEntry holds a registered SLA config alongside the workflow type name.
type slaEntry struct {
	workflowType string
	config       SLAConfig
}

// RegisterWorkflowWithSLA registers a workflow function with the Temporal worker
// AND registers an SLA config. SLA monitors start when engine.Start() is called.
//
// Usage:
//
//	engine.RegisterWorkflowWithSLA(LoanDisbursementWorkflow,
//	    temporalcommon.SLAConfig{
//	        WarnAfter:  5 * time.Minute,
//	        AlertAfter: 30 * time.Minute,
//	        AlertFn: func(ctx context.Context, evt temporalcommon.SLABreachEvent) {
//	            pagerduty.Alert(evt.WorkflowID, evt.RunningFor.String())
//	        },
//	    })
func (e *Engine) RegisterWorkflowWithSLA(workflowFn interface{}, cfg SLAConfig) {
	e.worker.RegisterWorkflow(workflowFn)

	if cfg.WarnAfter == 0 && cfg.AlertAfter == 0 {
		return
	}

	typeName := workflowFunctionName(workflowFn)
	e.slaEntries = append(e.slaEntries, slaEntry{workflowType: typeName, config: cfg})
}

// startSLAMonitors launches background goroutines for all registered SLA configs.
// Called from engine.Start() after the worker starts.
func (e *Engine) startSLAMonitors(ctx context.Context) {
	for _, entry := range e.slaEntries {
		entry := entry
		go e.runSLAMonitor(ctx, entry)
	}
}

// runSLAMonitor polls Temporal for open executions of a single workflow type
// and fires SLA breach callbacks when thresholds are exceeded.
func (e *Engine) runSLAMonitor(ctx context.Context, entry slaEntry) {
	cfg := entry.config

	// Poll at half the smaller threshold, minimum 30s.
	interval := 30 * time.Second
	if cfg.WarnAfter > 0 && cfg.WarnAfter/2 < interval {
		interval = cfg.WarnAfter / 2
	}
	if cfg.AlertAfter > 0 && cfg.AlertAfter/2 < interval {
		interval = cfg.AlertAfter / 2
	}
	if interval < 30*time.Second {
		interval = 30 * time.Second
	}

	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	// alerted tracks workflow IDs that already fired the alert-level callback
	// so we don't spam AlertFn on every poll cycle.
	alerted := make(map[string]bool)

	e.logger.Info("sla monitor started",
		zap.String("workflowType", entry.workflowType),
		zap.Duration("warnAfter", cfg.WarnAfter),
		zap.Duration("alertAfter", cfg.AlertAfter),
		zap.Duration("pollInterval", interval),
	)

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			e.checkSLA(ctx, entry, alerted)
		}
	}
}

// checkSLA lists open workflow executions and checks their start times.
func (e *Engine) checkSLA(ctx context.Context, entry slaEntry, alerted map[string]bool) {
	cfg := entry.config

	resp, err := e.client.ListOpenWorkflow(ctx, &workflowservice.ListOpenWorkflowExecutionsRequest{
		Namespace:       e.config.Namespace,
		MaximumPageSize: 1000,
		Filters: &workflowservice.ListOpenWorkflowExecutionsRequest_TypeFilter{
			TypeFilter: &filterpb.WorkflowTypeFilter{
				Name: entry.workflowType,
			},
		},
	})
	if err != nil {
		e.logger.Error("sla monitor: list open workflows failed",
			zap.String("workflowType", entry.workflowType),
			zap.Error(err),
		)
		return
	}

	for _, exec := range resp.GetExecutions() {
		startTime := exec.GetStartTime().AsTime()
		runningFor := time.Since(startTime)
		wfID := exec.GetExecution().GetWorkflowId()

		if cfg.AlertAfter > 0 && runningFor >= cfg.AlertAfter && !alerted[wfID] {
			event := SLABreachEvent{
				WorkflowID:   wfID,
				WorkflowType: entry.workflowType,
				RunningFor:   runningFor,
				Threshold:    cfg.AlertAfter,
				Level:        "alert",
			}
			e.logger.Error("sla breach: alert threshold exceeded",
				zap.String("workflowID", wfID),
				zap.Duration("runningFor", runningFor),
				zap.Duration("alertAfter", cfg.AlertAfter),
			)
			alerted[wfID] = true
			if cfg.AlertFn != nil {
				go cfg.AlertFn(ctx, event)
			}
			continue
		}

		if cfg.WarnAfter > 0 && runningFor >= cfg.WarnAfter {
			e.logger.Warn("sla breach: warn threshold exceeded",
				zap.String("workflowID", wfID),
				zap.Duration("runningFor", runningFor),
				zap.Duration("warnAfter", cfg.WarnAfter),
			)
		}
	}
}

// workflowFunctionName extracts the short name Temporal uses to identify a workflow type.
// Temporal registers workflows by the unqualified function name (e.g. "LoanDisbursementWorkflow").
func workflowFunctionName(fn interface{}) string {
	fullName := runtime.FuncForPC(reflect.ValueOf(fn).Pointer()).Name()
	// fullName is like "github.com/yourorg/temporal-common/examples/loan-disbursement.LoanDisbursementWorkflow"
	// Temporal uses only the part after the last dot.
	parts := strings.Split(fullName, ".")
	name := parts[len(parts)-1]
	if name == "" {
		return fmt.Sprintf("%T", fn)
	}
	return name
}
