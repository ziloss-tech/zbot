package reviewer

import (
	"fmt"

	"github.com/ziloss-tech/zbot/internal/agent"
)

type ReviewChunk struct {
	ID        string
	SessionID string
	Summary   string
	Actions   []string
	TokenEst  int
}

func ChunkRecentEvents(eventBus agent.EventBus, sessionID string, maxChunks int) []ReviewChunk {
	events := eventBus.Recent(sessionID, 50)
	if len(events) == 0 {
		return nil
	}

	var chunks []ReviewChunk
	var current ReviewChunk
	current.SessionID = sessionID
	current.ID = fmt.Sprintf("chunk-0")
	chunkIdx := 0

	for _, evt := range events {
		action := fmt.Sprintf("[%s] %s", evt.Type, evt.Summary)
		tokenEst := len(action) / 4

		if current.TokenEst+tokenEst > 2000 && len(current.Actions) > 0 {
			chunks = append(chunks, current)
			chunkIdx++
			if chunkIdx >= maxChunks {
				break
			}
			current = ReviewChunk{
				ID:        fmt.Sprintf("chunk-%d", chunkIdx),
				SessionID: sessionID,
			}
		}

		current.Actions = append(current.Actions, action)
		current.TokenEst += tokenEst
		if current.Summary == "" {
			current.Summary = evt.Summary
		}
	}

	if len(current.Actions) > 0 && len(chunks) < maxChunks {
		chunks = append(chunks, current)
	}

	return chunks
}
