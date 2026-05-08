package progress

import (
	"context"
	"fmt"
	"io"
)

type contextKey struct{}

type reporter struct {
	writer    io.Writer
	verbosity int
}

func WithWriter(ctx context.Context, w io.Writer) context.Context {
	if w == nil {
		return ctx
	}
	current, _ := ctx.Value(contextKey{}).(reporter)
	if current.writer == nil && current.verbosity == 0 {
		current.verbosity = 1
	}
	current.writer = w
	return context.WithValue(ctx, contextKey{}, current)
}

func WithReporter(ctx context.Context, w io.Writer, verbosity int) context.Context {
	if w == nil {
		return ctx
	}
	if verbosity < 0 {
		verbosity = 0
	}
	return context.WithValue(ctx, contextKey{}, reporter{
		writer:    w,
		verbosity: verbosity,
	})
}

func Printf(ctx context.Context, format string, args ...any) {
	PrintfLevel(ctx, 1, format, args...)
}

func PrintfLevel(ctx context.Context, level int, format string, args ...any) {
	if ctx == nil {
		return
	}
	current, _ := ctx.Value(contextKey{}).(reporter)
	if current.writer == nil || current.verbosity < level {
		return
	}
	_, _ = fmt.Fprintf(current.writer, format+"\n", args...)
}
