package client

import (
	"context"
	"os"
	"os/signal"
	"syscall"
	"time"

	"go.uber.org/zap"
)

// Start launches the Temporal worker and blocks until ctx is cancelled or
// SIGTERM/SIGINT is received. Performs graceful shutdown within ShutdownTimeout.
//
// Typical usage:
//
//	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
//	defer stop()
//	if err := engine.Start(ctx); err != nil {
//	    log.Fatal(err)
//	}
func (e *Engine) Start(ctx context.Context) error {
	if err := e.worker.Start(); err != nil {
		return err
	}
	e.logger.Info("temporal worker started",
		zap.String("taskQueue", e.config.TaskQueue),
		zap.String("namespace", e.config.Namespace),
		zap.String("hostPort", e.config.HostPort),
	)

	// Launch SLA monitors for any workflows registered with RegisterWorkflowWithSLA.
	e.startSLAMonitors(ctx)

	// Listen for context cancellation or OS signals.
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGTERM, syscall.SIGINT)
	defer signal.Stop(sigCh)

	select {
	case <-ctx.Done():
		e.logger.Info("context cancelled, shutting down")
	case sig := <-sigCh:
		e.logger.Info("received signal, shutting down", zap.String("signal", sig.String()))
	}

	return e.shutdown()
}

// shutdown stops the worker and closes the client within ShutdownTimeout.
func (e *Engine) shutdown() error {
	shutdownCtx, cancel := context.WithTimeout(context.Background(), e.config.ShutdownTimeout)
	defer cancel()

	e.logger.Info("stopping worker", zap.Duration("timeout", e.config.ShutdownTimeout))
	e.worker.Stop()
	e.logger.Info("worker stopped")

	e.client.Close()
	e.logger.Info("temporal client closed")

	if err := e.tracingShutdown(shutdownCtx); err != nil {
		e.logger.Warn("tracing shutdown error", zap.Error(err))
	}

	_ = e.logger.Sync()
	return nil
}

// Shutdown performs a graceful shutdown without blocking on OS signals.
// Use this when you manage the lifecycle externally (e.g., in tests or
// custom signal handling).
func (e *Engine) Shutdown(ctx context.Context) error {
	timeoutCtx, cancel := context.WithTimeout(ctx, e.config.ShutdownTimeout)
	defer cancel()

	e.worker.Stop()
	e.client.Close()

	if err := e.tracingShutdown(timeoutCtx); err != nil {
		e.logger.Warn("tracing shutdown error", zap.Error(err))
	}

	_ = e.logger.Sync()
	return nil
}

// DrainTimeout returns the configured shutdown drain period.
// Useful for health check endpoints that need to report "shutting down" status.
func (e *Engine) DrainTimeout() time.Duration {
	return e.config.ShutdownTimeout
}
