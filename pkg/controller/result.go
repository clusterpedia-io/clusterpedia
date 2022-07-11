package controller

type Result interface {
	Requeue() bool
	MaxRetryCount() int
}

type requeueResult int

func RequeueResult(retry int) Result {
	return requeueResult(retry)
}

func (r requeueResult) Requeue() bool {
	return true
}

func (r requeueResult) MaxRetryCount() int {
	return int(r)
}

type noRequeueResult struct{}

var NoRequeueResult Result = noRequeueResult{}

func (r noRequeueResult) Requeue() bool {
	return false
}

func (r noRequeueResult) MaxRetryCount() int {
	return 0
}
