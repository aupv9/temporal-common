package main

import (
	"context"
	"log"

	temporalcommon "github.com/yourorg/temporal-common"
	loandisbursement "github.com/yourorg/temporal-common/examples/loan-disbursement"
)

func main() {
	engine, err := temporalcommon.New(temporalcommon.Config{
		HostPort:  "localhost:7233",
		Namespace: "default",
		TaskQueue: loandisbursement.TaskQueue,
		Metrics:   temporalcommon.MetricsConfig{Enabled: true},
	})
	if err != nil {
		log.Fatalf("create engine: %v", err)
	}

	engine.RegisterWorkflow(loandisbursement.LoanDisbursementWorkflow)
	engine.RegisterActivity(loandisbursement.KYCCheckActivity)
	engine.RegisterActivity(loandisbursement.ReserveFundsActivity)
	engine.RegisterActivity(loandisbursement.DisbursementActivity)
	engine.RegisterActivity(loandisbursement.NotifyBorrowerActivity)
	engine.RegisterActivity(loandisbursement.ReleaseReservationActivity)

	if err := engine.Start(context.Background()); err != nil {
		log.Fatalf("engine stopped: %v", err)
	}
}
