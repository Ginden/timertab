package progress

import (
	"context"
	"fmt"
	"io"
)

type contextKey struct{}

func WithWriter(ctx context.Context, w io.Writer) context.Context {
	if w == nil {
		return ctx
	}
	return context.WithValue(ctx, contextKey{}, w)
}

func Printf(ctx context.Context, format string, args ...any) {
	if ctx == nil {
		return
	}
	writer, _ := ctx.Value(contextKey{}).(io.Writer)
	if writer == nil {
		return
	}
	_, _ = fmt.Fprintf(writer, format+"\n", args...)
}
