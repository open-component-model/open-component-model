package status

import (
	"time"

	ctrl "sigs.k8s.io/controller-runtime"
)

// RequeueResult returns a ctrl.Result based on the condition state of the object.
// If the object is ready and has a positive interval, it requeues after the interval.
// If the object is stalled, it does not requeue (terminal state).
// Otherwise it returns an empty result, letting controller-runtime handle error-based requeue.
func RequeueResult(obj ConditionObject, interval time.Duration) ctrl.Result {
	if IsReady(obj) && interval > 0 {
		return ctrl.Result{RequeueAfter: interval}
	}
	if IsStalled(obj) {
		return ctrl.Result{}
	}
	return ctrl.Result{}
}
