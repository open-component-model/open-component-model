// Package workerpool implements the asynchronous transfer worker pool for the
// replication controller (ADR 0020).
//
// The pool follows the async pattern of the resolution worker pool but
// specializes for transfers:
//
//   - Work is keyed by the Replication CR UID, so burst reconciles for an
//     already-submitted transfer collapse onto a single in-flight item and
//     return [ErrTransferInProgress] instead of re-submitting.
//   - There is no work queue. Each accepted transfer runs in its own goroutine
//     gated by a concurrency semaphore. Because work is deduplicated per key,
//     in-flight work is bounded by the number of Replication CRs, so a bounded
//     queue and a queue-full error path are unnecessary.
//   - Results are one-shot and consumed exactly once via [WorkerPool.Result];
//     there is no result cache. Durable de-duplication of already-transferred
//     content lives in the CR status (lastTransferredDigest) and in the
//     content-addressed target registry, not in the pool.
//   - A result carries the opaque Stamp it was submitted with. Because the key
//     is the CR UID and not the unit of work, a result can outlive the source
//     state it was produced for (request coalescing, a dropped event, a resync
//     landing after the digest moved). [WorkerPool.Result] takes the currently
//     desired Stamp and returns a result only on a match, dropping a superseded
//     one so the caller cannot accidentally finalize stale work. The pool is
//     keyed by UID rather than content on purpose: content keying would share a
//     transfer across objects with different credentials, letting one object
//     free-ride on another's write capability. Also, if a digest changes in-between
//     it would be possible to fetch stale data. State A -> Request Fetch ->
//     return InProgress -> State A transition to State B -> Request Fetch ->
//     found match for UID ( that is State A ) -> return stale state.
//   - In-flight transfers are cancelable per key via [WorkerPool.Cancel] to
//     support the deletion drain described in the ADR. A transfer still waiting
//     for a semaphore slot aborts immediately on cancel.
//   - On completion the pool emits one generic event per requester on the
//     channel returned by [WorkerPool.Events]. The controller wires it with
//     controller-runtime's source.Channel and handler.EnqueueRequestForObject
//     to retrigger reconciliation.
//
// Phase 1 (building the in-memory TransformationGraphDefinition) happens in the
// reconciler. The pool runs Phase 2 only: it builds the executable graph from
// the TGD and processes it.
package workerpool
