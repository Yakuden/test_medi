package observability

import (
	"context"
	"strings"

	"go.uber.org/zap"
)

type Logger struct {
	z *zap.Logger
}

func NewLogger(service string) (*Logger, error) {
	z, err := zap.NewProduction()
	if err != nil {
		return nil, err
	}
	return &Logger{z: z.With(zap.String("service", service))}, nil
}

func (l *Logger) With(fields ...zap.Field) *Logger {
	return &Logger{z: l.z.With(fields...)}
}

func (l *Logger) withCtx(ctx context.Context) *zap.Logger {
	tid := TraceIDFromContext(ctx)
	if tid == "" {
		return l.z
	}
	return l.z.With(
		zap.String("trace_id", tid),
		zap.String("span_id", SpanIDFromContext(ctx)),
	)
}

func (l *Logger) Info(ctx context.Context, msg string, fields ...zap.Field) {
	l.withCtx(ctx).Info(msg, fields...)
}

func (l *Logger) Error(ctx context.Context, msg string, fields ...zap.Field) {
	l.withCtx(ctx).Error(msg, fields...)
}

func (l *Logger) Warn(ctx context.Context, msg string, fields ...zap.Field) {
	l.withCtx(ctx).Warn(msg, fields...)
}

func (l *Logger) Debug(ctx context.Context, msg string, fields ...zap.Field) {
	l.withCtx(ctx).Debug(msg, fields...)
}

func (l *Logger) Sync() error { return l.z.Sync() }

func Redact(s string) string {
	if len(s) <= 4 {
		return strings.Repeat("*", len(s))
	}
	return s[:2] + strings.Repeat("*", len(s)-4) + s[len(s)-2:]
}

func MaskedEmail(email string) string {
	i := strings.IndexByte(email, '@')
	if i < 0 {
		return "***"
	}
	return Redact(email[:i]) + email[i:]
}

func MaskedCard(card string) string {
	d := strings.ReplaceAll(card, " ", "")
	if len(d) < 4 {
		return "****"
	}
	return strings.Repeat("*", len(d)-4) + d[len(d)-4:]
}
