# Retry Presets

All presets are defined in `activity/options.go`. Never define `ActivityOptions` manually in business workflows — use these presets.

## Preset Table

| Preset | ScheduleToClose | StartToClose | Heartbeat | MaxAttempts | InitialInterval | Backoff |
|---|---|---|---|---|---|---|
| `WithFinancialAPIOptions` | 5 min | 2 min | 30s | 5 | 2s | ×2, max 30s |
| `WithNotificationOptions` | 2 min | 1 min | — | 3 | 5s | ×1 (fixed) |
| `WithInternalServiceOptions` | 1 min | 30s | — | 10 | 500ms | ×2, max 10s |
| `WithLongRunningOptions` | 24 hr | 23 hr | 60s | 3 | 10s | ×2, max 5 min |

## Non-Retryable Error Codes

All presets treat these error types as non-retryable (Temporal will not retry):

```
InsufficientFund
BorrowerBlacklisted
LoanAlreadyDisbursed
DuplicateTransaction
InvalidLoanAmount
ExpiredOffer
CompensationError
```

To add a new business error code:
1. Add the constructor in `activity/errors.go` using `NewBusinessError("YourCode", ...)`
2. Add the code string to `businessErrorCodes` in `activity/options.go`

## Preset Selection Guide

- **External bank / payment gateway** → `WithFinancialAPIOptions` (moderate retry, heartbeat required)
- **Email / SMS / push** → `WithNotificationOptions` (best-effort, never block saga)
- **Internal gRPC / HTTP service** → `WithInternalServiceOptions` (aggressive retry, fast recovery)
- **Batch jobs / data exports** → `WithLongRunningOptions` (heartbeat required, rare retry)

## Compensation Activities

Do NOT use the above presets for compensation activities. The `Saga` manager applies its own options with `MaximumAttempts: 0` (unlimited) and no `NonRetryableErrorTypes`.
