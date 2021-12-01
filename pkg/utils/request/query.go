package request

import (
	"context"
	"net/url"
)

const queryKey = "request-query"

func WithRequestQuery(parent context.Context, query url.Values) context.Context {
	return context.WithValue(parent, queryKey, query)
}

func RequestQueryFrom(ctx context.Context) url.Values {
	query, _ := ctx.Value(queryKey).(url.Values)
	return query
}
