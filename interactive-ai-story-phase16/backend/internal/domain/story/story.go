package story

import (
	"errors"
	"strings"
	"time"

	"github.com/local/interactive-ai-story/backend/internal/domain/id"
)

type ID id.ID
type UserID id.ID

type Status string

const (
	StatusActive   Status = "active"
	StatusArchived Status = "archived"
	StatusDeleted  Status = "deleted"
)

var ErrInvalidStory = errors.New("invalid story")

type Story struct {
	ID               ID
	OwnerID          UserID
	Title            string
	Description      string
	Status           Status
	SemanticRevision int64
	CreatedAt        time.Time
	UpdatedAt        time.Time
}

func New(storyID ID, ownerID UserID, title string, now time.Time) (Story, error) {
	title = strings.TrimSpace(title)
	if id.ID(storyID).IsZero() || id.ID(ownerID).IsZero() || title == "" {
		return Story{}, ErrInvalidStory
	}
	return Story{ID: storyID, OwnerID: ownerID, Title: title, Status: StatusActive, SemanticRevision: 1, CreatedAt: now, UpdatedAt: now}, nil
}

func (s Story) Validate() error {
	if id.ID(s.ID).IsZero() || id.ID(s.OwnerID).IsZero() || strings.TrimSpace(s.Title) == "" || s.SemanticRevision < 1 {
		return ErrInvalidStory
	}
	switch s.Status {
	case StatusActive, StatusArchived, StatusDeleted:
		return nil
	default:
		return ErrInvalidStory
	}
}
