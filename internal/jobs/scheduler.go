package jobs

import "context"

type Job interface {
	Name() string
	Run(context.Context) error
}

// Scheduler is deliberately only a boundary in the scaffold. The implementation
// should persist job runs/status and prevent overlapping source syncs.
type Scheduler interface {
	Enqueue(context.Context, Job) error
}
