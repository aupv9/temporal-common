package workflow

import (
	"context"
	"fmt"
	"time"

	enumspb "go.temporal.io/api/enums/v1"
	"go.temporal.io/sdk/client"
	"go.temporal.io/sdk/workflow"
)

// ScheduleOptions configures a Temporal Schedule for recurring workflow execution.
type ScheduleOptions struct {
	// ScheduleID is a unique identifier for the schedule. Required.
	ScheduleID string

	// CronExpression is a standard cron string, e.g. "0 9 * * MON-FRI".
	// Mutually exclusive with Interval.
	CronExpression string

	// Interval triggers the workflow every fixed duration, e.g. 15*time.Minute.
	// Mutually exclusive with CronExpression.
	Interval time.Duration

	// StartTime constrains when the schedule begins firing. Zero = immediately.
	StartTime time.Time

	// EndTime constrains when the schedule stops firing. Zero = no end.
	EndTime time.Time

	// TaskQueue overrides the Engine's default task queue for this schedule.
	TaskQueue string

	// WorkflowExecutionTimeout caps total execution time for each triggered run.
	WorkflowExecutionTimeout time.Duration
}

// CreateSchedule registers a recurring workflow schedule with the Temporal server.
// Default overlap policy is Skip (new run is skipped if previous is still running).
//
// Example:
//
//	err := workflow.CreateSchedule(ctx, engine.Client(), "my-queue", DailyReportWorkflow,
//	    DailyReportInput{},
//	    temporalcommon.ScheduleOptions{
//	        ScheduleID:     "daily-report",
//	        CronExpression: "0 6 * * *",
//	    },
//	)
func CreateSchedule(
	ctx context.Context,
	c client.Client,
	taskQueue string,
	workflowFn interface{},
	workflowArgs interface{},
	opts ScheduleOptions,
) error {
	if opts.ScheduleID == "" {
		return fmt.Errorf("CreateSchedule: ScheduleID is required")
	}
	if opts.CronExpression == "" && opts.Interval == 0 {
		return fmt.Errorf("CreateSchedule: one of CronExpression or Interval is required")
	}

	tq := opts.TaskQueue
	if tq == "" {
		tq = taskQueue
	}

	spec := client.ScheduleSpec{}
	if opts.CronExpression != "" {
		spec.CronExpressions = []string{opts.CronExpression}
	} else {
		spec.Intervals = []client.ScheduleIntervalSpec{
			{Every: opts.Interval},
		}
	}
	if !opts.StartTime.IsZero() {
		spec.StartAt = opts.StartTime
	}
	if !opts.EndTime.IsZero() {
		spec.EndAt = opts.EndTime
	}

	_, err := c.ScheduleClient().Create(ctx, client.ScheduleOptions{
		ID:   opts.ScheduleID,
		Spec: spec,
		Action: &client.ScheduleWorkflowAction{
			Workflow:                 workflowFn,
			Args:                     []interface{}{workflowArgs},
			TaskQueue:                tq,
			WorkflowExecutionTimeout: opts.WorkflowExecutionTimeout,
		},
		Overlap: enumspb.SCHEDULE_OVERLAP_POLICY_SKIP,
	})
	if err != nil {
		return fmt.Errorf("create schedule %q: %w", opts.ScheduleID, err)
	}
	return nil
}

// DeleteSchedule removes a schedule by ID.
func DeleteSchedule(ctx context.Context, c client.Client, scheduleID string) error {
	handle := c.ScheduleClient().GetHandle(ctx, scheduleID)
	if err := handle.Delete(ctx); err != nil {
		return fmt.Errorf("delete schedule %q: %w", scheduleID, err)
	}
	return nil
}

// Now re-exports workflow.Now so workflow code can import from one place.
func Now(ctx workflow.Context) time.Time {
	return workflow.Now(ctx)
}
