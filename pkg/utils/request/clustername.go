package request

import "context"

const clusterNameKey = "cluster-name"

func WithClusterName(parent context.Context, name string) context.Context {
	return context.WithValue(parent, clusterNameKey, name)
}

func ClusterNameFrom(ctx context.Context) (string, bool) {
	name, ok := ctx.Value(clusterNameKey).(string)
	return name, ok
}

func ClusterNameValue(ctx context.Context) string {
	name, _ := ClusterNameFrom(ctx)
	return name
}
