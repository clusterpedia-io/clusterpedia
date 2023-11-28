package pager

import (
	"context"

	"k8s.io/apimachinery/pkg/runtime"
)

type resultStream int

const resultStreamKey resultStream = iota

func WithResultStream(parent context.Context, ch chan runtime.Object) context.Context {
	return context.WithValue(parent, resultStreamKey, ch)
}

func ResultStream(ctx context.Context) chan runtime.Object {
	ch, _ := ctx.Value(resultStreamKey).(chan runtime.Object)
	return ch
}
