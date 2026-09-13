package planning

import (
	"encoding/json"

	"github.com/zhanshimian/server/internal/domain"
)

// planningMemoryLimit is how many recent preference memories seed one planning
// run; the repository orders them recent-first before this cap is applied.
const planningMemoryLimit = 20

// embedFeedbackMemory merges the deterministic feedback_memory snapshot into the
// profile snapshot handed to the generator. The planner grounds affected steps
// on memory IDs, and the validation gate resolves those IDs against this same
// snapshot.
func embedFeedbackMemory(profileSnapshot json.RawMessage, memories []domain.PreferenceMemory) (json.RawMessage, error) {
	items := make([]domain.FeedbackMemoryItem, 0, len(memories))
	for _, memory := range memories {
		items = append(items, domain.FeedbackMemoryItem{
			ID: memory.ID, Key: memory.Key, Category: string(memory.Category), Value: memory.Value,
		})
	}
	encoded, err := json.Marshal(domain.FeedbackMemorySnapshot{Items: items})
	if err != nil {
		return nil, err
	}
	root := map[string]json.RawMessage{}
	if len(profileSnapshot) > 0 {
		if err := json.Unmarshal(profileSnapshot, &root); err != nil {
			return nil, err
		}
	}
	root["feedback_memory"] = encoded
	return json.Marshal(root)
}
