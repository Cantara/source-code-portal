// Package logx: STUB. Only RequestID/WithRequestID are used by this unit.
package logx

import "context"

type ctxKey struct{}

func WithRequestID(ctx context.Context, id string) context.Context {
	return context.WithValue(ctx, ctxKey{}, id)
}
func RequestID(ctx context.Context) string { s, _ := ctx.Value(ctxKey{}).(string); return s }
