package request

import "context"

type mediaTypeKeyType int

const mediaTypeKindKey mediaTypeKeyType = iota

func WithExtraMediaTypeKind(parent context.Context, kind string) context.Context {
	if kind == "" {
		return parent
	}
	return context.WithValue(parent, mediaTypeKindKey, kind)
}

func ExtraMediaTypeKindFrom(ctx context.Context) (string, bool) {
	name, ok := ctx.Value(mediaTypeKindKey).(string)
	return name, ok
}

func ExtraMediaTypeKindValue(ctx context.Context) string {
	name, _ := ExtraMediaTypeKindFrom(ctx)
	return name
}
