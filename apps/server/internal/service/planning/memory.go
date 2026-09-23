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

// planningDecisionLimit is how many recent variant decisions seed one planning
// run; the repository orders them recent-first before this cap is applied.
const planningDecisionLimit = 20

// decisionMemorySnapshot is the deterministic shape embedded under the
// decision_memory root key. It is plain context for the generator: no
// grounding source type exists for it, so the model can read it but can only
// cite it through the legitimate profile_preference pointer path.
type decisionMemorySnapshot struct {
	Items []domain.VariantDecisionItem `json:"items"`
}

// embedDecisions merges the deterministic decision snapshot into the profile
// snapshot as a sibling root key of feedback_memory (worker hand-off, same
// seam as embedFeedbackMemory).
func embedDecisions(profileSnapshot json.RawMessage, items []domain.VariantDecisionItem) (json.RawMessage, error) {
	encoded, err := json.Marshal(decisionMemorySnapshot{Items: items})
	if err != nil {
		return nil, err
	}
	root := map[string]json.RawMessage{}
	if len(profileSnapshot) > 0 {
		if err := json.Unmarshal(profileSnapshot, &root); err != nil {
			return nil, err
		}
	}
	root["decision_memory"] = encoded
	return json.Marshal(root)
}
