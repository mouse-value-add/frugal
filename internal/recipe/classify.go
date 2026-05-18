package recipe

import "strings"

// Classify picks the recipe whose `classifier.hints` best match the task
// string, returning (id, true) on a hit and ("", false) when no recipe
// matches any hint. The caller decides what to do on a miss — most paths
// fall back to a sensible default (e.g., the "general-chat" recipe) or
// surface a "no recipe matches; pass --recipe to pick explicitly" error.
//
// Today's matching rule is intentionally simple: case-insensitive substring
// match on each hint, score = number of distinct hints matched, ties broken
// by registry load order. Good enough for the four starter recipes;
// upgrades (token-overlap, learned classifier) land when the eval shows the
// rule-based matcher misroutes meaningful workload share.
func (r *Registry) Classify(task string) (string, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	if task == "" || len(r.recipes) == 0 {
		return "", false
	}
	lower := strings.ToLower(task)

	bestID := ""
	bestScore := 0
	bestOrder := -1
	for idx, id := range r.order {
		rec := r.recipes[id]
		score := 0
		for _, h := range rec.Classifier.Hints {
			if h == "" {
				continue
			}
			if strings.Contains(lower, strings.ToLower(h)) {
				score++
			}
		}
		if score == 0 {
			continue
		}
		// Strictly greater wins; equal score keeps the earlier-loaded
		// recipe (lower idx) — sort-by-name gives a stable answer.
		if score > bestScore || (score == bestScore && (bestOrder < 0 || idx < bestOrder)) {
			bestID = id
			bestScore = score
			bestOrder = idx
		}
	}
	if bestScore == 0 {
		return "", false
	}
	return bestID, true
}
