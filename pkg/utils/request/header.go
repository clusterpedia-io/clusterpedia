package request

import (
	"context"
)

type headerKeyType int

const acceptHeaderKey headerKeyType = iota

func WithAcceptHeader(parent context.Context, accept string) context.Context {
	if accept == "" {
		return parent
	}
	return context.WithValue(parent, acceptHeaderKey, accept)
}

func AcceptHeaderFrom(ctx context.Context) string {
	query, _ := ctx.Value(acceptHeaderKey).(string)
	return query
}
