package journal_entry

import (
	"time"

	"github.com/gofrs/uuid"
)

type Emotion string

const (
	EmotionCalm       Emotion = "calm"
	EmotionNeutral    Emotion = "neutral"
	EmotionFomo       Emotion = "fomo"
	EmotionAnxious    Emotion = "anxious"
	EmotionConfident  Emotion = "confident"
	EmotionFearful    Emotion = "fearful"
	EmotionGreedy     Emotion = "greedy"
)

type JournalEntry struct {
	ID          uuid.UUID
	TradeID     uuid.UUID
	TenantID    uuid.UUID
	UserID      uuid.UUID
	Strategy    *string
	EntryReason *string
	ExitReason  *string
	Emotion     *Emotion
	Mistake     *string
	Notes       *string
	Metadata    map[string]any
	CreatedAt   time.Time
	UpdatedAt   time.Time
}