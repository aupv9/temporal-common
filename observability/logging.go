package observability

import (
	"go.temporal.io/sdk/log"
	"go.uber.org/zap"
)

// zapLogger adapts go.uber.org/zap to Temporal's log.Logger interface.
type zapLogger struct {
	zap *zap.Logger
}

// NewZapLogger wraps a *zap.Logger so it can be passed to client.Options.Logger.
func NewZapLogger(z *zap.Logger) log.Logger {
	return &zapLogger{zap: z}
}

func (l *zapLogger) Debug(msg string, keyvals ...interface{}) {
	l.zap.Debug(msg, toFields(keyvals)...)
}

func (l *zapLogger) Info(msg string, keyvals ...interface{}) {
	l.zap.Info(msg, toFields(keyvals)...)
}

func (l *zapLogger) Warn(msg string, keyvals ...interface{}) {
	l.zap.Warn(msg, toFields(keyvals)...)
}

func (l *zapLogger) Error(msg string, keyvals ...interface{}) {
	l.zap.Error(msg, toFields(keyvals)...)
}

// toFields converts Temporal's variadic key-value pairs to zap.Field slice.
// Keys must be strings; values can be anything.
func toFields(keyvals []interface{}) []zap.Field {
	if len(keyvals) == 0 {
		return nil
	}
	fields := make([]zap.Field, 0, len(keyvals)/2)
	for i := 0; i+1 < len(keyvals); i += 2 {
		key, ok := keyvals[i].(string)
		if !ok {
			key = "unknown"
		}
		fields = append(fields, zap.Any(key, keyvals[i+1]))
	}
	// If odd number of keyvals, attach the last one with a sentinel key.
	if len(keyvals)%2 != 0 {
		fields = append(fields, zap.Any("extra", keyvals[len(keyvals)-1]))
	}
	return fields
}
