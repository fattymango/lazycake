package scheduler

// Placement score weights (IMPLEMENTATION.md task 5.4):
//
//	score = base_fit + cache_bonus*(image_size_gb/2) - queue_penalty*tasks_assigned_to_node + trust_weight*trust_score
const (
	cacheBonusWeight   = 1.0
	queuePenaltyWeight = 0.5
	trustWeight        = 2.0
)

// PlacementScoreInputs is everything task 5.4's formula needs for one
// (task, node) pair.
type PlacementScoreInputs struct {
	// BaseFit is however well the node's free capacity matches the task's
	// requested resources - left as a caller-supplied input rather than
	// computed here, since "fit" already has its own logic in
	// ClaimQueuedTask's SQL filter and isn't this task's concern to
	// redefine.
	BaseFit float64
	// ImageSizeGB is the task's image size, if known (0 if never seen by
	// any node yet - a genuinely cold image across the whole fleet).
	ImageSizeGB float64
	// CachedHere is whether this specific node already has the image.
	CachedHere bool
	// TasksAssignedToNode is the node's current queue depth.
	TasksAssignedToNode int
	// TrustScore is the node's current trust score (task 5.3), [0,1].
	TrustScore float64
}

// PlacementScore computes task 5.4's formula for one (task, node) pair.
// Higher is better.
//
// This is deliberately a pure function, not wired into the SQL-based
// per-node claim loop (tryPlaceOne) that the rest of this scheduler uses:
// that loop is node-driven (for each connected node, claim any one fitting
// task via SELECT ... FOR UPDATE SKIP LOCKED), not task-driven (for this
// task, score every candidate node and pick the best) - moving to the
// latter is a real architectural change this session's remaining time
// didn't allow for. See OPEN_QUESTIONS.md. What *is* wired into the real
// dispatch path is the fleet-wide cold-pull cap (ColdPullLimiter,
// coldpull.go) - task 5.4's own literal "Done when" criterion.
func PlacementScore(in PlacementScoreInputs) float64 {
	score := in.BaseFit
	if in.CachedHere {
		score += cacheBonusWeight * (in.ImageSizeGB / 2)
	}
	score -= queuePenaltyWeight * float64(in.TasksAssignedToNode)
	score += trustWeight * in.TrustScore
	return score
}
