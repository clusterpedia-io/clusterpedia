package norm

import (
	"fmt"

	"github.com/zhihu/norm/constants"
)

type (
	ITag interface {
		TagName() string
	}
	IVertex interface {
		ITag
		GetVid() interface{}
		GetPolicy() constants.Policy
	}
	// vid is int or string
	VModel struct {
		Vid    interface{}      `norm:"-"`
		Policy constants.Policy `norm:"-"`
	}
)

var _ IVertex = new(VModel)

func (v VModel) TagName() string {
	// TODO return model name with snake style
	panic("")
}

func (v VModel) GetVid() interface{} {
	return v.Vid
}

func (v VModel) GetPolicy() constants.Policy {
	return v.Policy
}

type (
	IEdge interface {
		// 返回边的名称
		EdgeName() string
		GetVidSrc() interface{}
		GetVidSrcPolicy() constants.Policy
		GetVidDst() interface{}
		GetVidDstPolicy() constants.Policy
	}
	EModel struct {
		Src       interface{}      `norm:"-"`
		SrcPolicy constants.Policy `norm:"-"`
		Dst       interface{}      `norm:"-"`
		DstPolicy constants.Policy `norm:"-"`
	}
)

var _ IEdge = new(EModel)

func (v EModel) EdgeName() string {
	panic("")
}

func (v EModel) GetVidSrc() interface{} {
	return v.Src
}

func (v EModel) GetVidSrcPolicy() constants.Policy {
	return v.SrcPolicy
}

func (v EModel) GetVidDst() interface{} {
	return v.Dst
}

func (v EModel) GetVidDstPolicy() constants.Policy {
	return v.DstPolicy
}

func GetVidWithPolicy(vid interface{}, policy constants.Policy) string {
	vidStr := ""
	switch vid := vid.(type) {
	case int, int8, int32, int64, float32, float64:
		vidStr = fmt.Sprint(vid)
	case string:
		vidStr = "'" + vid + "'"
	default:
		vidStr += "'" + fmt.Sprint(vid) + "'"
	}
	switch policy {
	case constants.PolicyHash:
		vidStr = "hash(" + vidStr + ")"
	}
	return vidStr
}
