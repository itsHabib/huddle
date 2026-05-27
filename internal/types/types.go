// Package types holds shared payloads used across MCP handlers and adapters.
package types

import "time"

// Identity classifications for MCP and Slack-rendered attribution.
const (
	IdentityKindSeat         = "seat"
	IdentityKindOrchestrator = "orchestrator"
	IdentityKindHuman        = "human"
	IdentityKindUnknown      = "unknown"
)

// Identity represents who produced a logical message inside a huddle.
type Identity struct {
	Kind        string `json:"kind"`
	DisplayName string `json:"displayName,omitempty"`
	SeatID      string `json:"seatId,omitempty"`
}

// Message is one channel utterance surfaced to MCP clients.
type Message struct {
	ID        string    `json:"id"`
	Body      string    `json:"body"`
	PostedAt  time.Time `json:"postedAt,omitzero"`
	ThreadTS  string    `json:"threadTs,omitempty"`
	SubType   string    `json:"subType,omitempty"`
	Identity  Identity  `json:"identity"`
	UserIDRaw string    `json:"-"`
}

// Seat is metadata for one joined agent without exposing key material over MCP.
type Seat struct {
	ID          string `json:"id"`
	DisplayName string `json:"displayName"`
}

// Huddle is persisted huddle meta without API keys or seat secrets.
type Huddle struct {
	ID                      string     `json:"id"`
	Purpose                 string     `json:"purpose"`
	OrchestratorID          string     `json:"orchestratorId"`
	OrchestratorDisplayName string     `json:"orchestratorDisplayName"`
	SlackChannelID          string     `json:"slackChannelId"`
	SlackChannelName        string     `json:"slackChannelName"`
	CreatedAt               time.Time  `json:"createdAt,omitzero"`
	ClosedAt                *time.Time `json:"closedAt,omitempty,omitzero"`
	TTLHours                *int       `json:"ttlHours,omitempty"`
}

// Verb argument and result scaffolding (validated further in handler streams).

// CreateArgs binds huddle creation input.
type CreateArgs struct {
	Purpose      string           `json:"purpose"`
	Orchestrator Seat             `json:"orchestrator"`
	Seats        []SeatDefinition `json:"seats"`
	TTLHours     *int             `json:"ttlHours,omitempty"`
}

// SeatDefinition is a logical seat declaration before keys exist.
type SeatDefinition struct {
	ID          string `json:"id"`
	DisplayName string `json:"displayName"`
}

// CreateResult is emitted after provisioning (handler stream).
type CreateResult struct {
	HuddleID     string        `json:"huddleId"`
	Channel      string        `json:"channel"`
	Orchestrator Seat          `json:"orchestrator"`
	Seats        []CreatedSeat `json:"seats"`
}

// CreatedSeat includes issuance material for MCP clients.
type CreatedSeat struct {
	ID          string `json:"id"`
	Key         string `json:"key"`
	DisplayName string `json:"displayName"`
}

// CloseArgs targets an existing huddle.
type CloseArgs struct {
	HuddleID string `json:"huddleId"`
}

// CloseResult reports shutdown success.
type CloseResult struct {
	Closed          bool   `json:"closed"`
	ArchivedChannel string `json:"archivedChannel,omitempty"`
}

// ListArgs selects which huddle rows surface. Active is optional: nil
// returns all huddles; *true returns only open ones; *false is equivalent
// to nil. Pointer type so the MCP schema reflects optionality.
type ListArgs struct {
	Active *bool `json:"active,omitempty"`
}

// PostArgs routes posts from seats or orchestrator paths (handler validation later).
type PostArgs struct {
	Key      string `json:"key,omitempty"`
	HuddleID string `json:"huddleId,omitempty"`
	Body     string `json:"body"`
	ReplyTo  string `json:"replyTo,omitempty"`
}

// PostResult echoes posted envelope metadata.
type PostResult struct {
	MessageID string    `json:"messageId,omitzero"`
	PostedAt  time.Time `json:"postedAt,omitzero"`
	Identity  Identity  `json:"identity"`
}

// ReadArgs fetches backlog windows.
type ReadArgs struct {
	Key      string     `json:"key,omitempty"`
	HuddleID string     `json:"huddleId,omitempty"`
	Since    *time.Time `json:"since,omitempty,omitzero"`
	Limit    int        `json:"limit,omitempty"`
}

// ReadResult is the object-shaped MCP output for huddle.read (SDK requires a JSON object).
type ReadResult struct {
	Messages []Message `json:"messages"`
}

// WhoElseArgs ties to a seating key today.
type WhoElseArgs struct {
	Key string `json:"key"`
}

// WhoElseResult summarizes who shares the slice.
type WhoElseResult struct {
	Purpose      string `json:"purpose"`
	Orchestrator Seat   `json:"orchestrator"`
	Seats        []Seat `json:"seats"`
}
