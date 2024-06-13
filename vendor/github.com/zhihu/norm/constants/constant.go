package constants

type Direction = string
type Policy int

const (
	// 反向
	DirectionReversely = "REVERSELY"
	// 双向
	DirectionBidirect = "BIDIRECT"
	StructTagName     = "norm"
)

const (
	PolicyNothing = iota
	PolicyHash
)
