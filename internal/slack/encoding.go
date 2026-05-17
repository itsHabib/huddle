// Package slack implements Slack transport helpers including message prefix encoding.
package slack

import (
	"strings"

	"github.com/itsHabib/huddle/internal/types"
)

// Encode renders an Identity + body according to docs/design § Slack message encoding.
func Encode(identity types.Identity, body string) string {
	switch identity.Kind {
	case types.IdentityKindOrchestrator:
		return "*[" + identity.DisplayName + "] " + body

	case types.IdentityKindSeat:
		return "[" + identity.DisplayName + "] " + body

	default:
		return body
	}
}

// Decode recovers attribution from the prefix rules; malformed bracket prefixes become unknown kind.
func Decode(text string) (types.Identity, string) {
	trimmed := strings.TrimSpace(text)

	if trimmed == "" {
		return types.Identity{Kind: types.IdentityKindHuman}, ""
	}

	if strings.HasPrefix(trimmed, "*[") {
		closeIdx := strings.IndexByte(trimmed[2:], ']')
		if closeIdx < 0 {
			return types.Identity{Kind: types.IdentityKindUnknown}, text
		}

		display := trimmed[2 : 2+closeIdx]

		body := trimmed[2+closeIdx+1:]
		body = consumeOptionalSpace(body)

		return types.Identity{Kind: types.IdentityKindOrchestrator, DisplayName: display}, body
	}

	if strings.HasPrefix(trimmed, "[") {
		closeIdx := strings.IndexByte(trimmed[1:], ']')
		if closeIdx < 0 {
			return types.Identity{Kind: types.IdentityKindUnknown}, text
		}

		display := trimmed[1 : 1+closeIdx]

		body := trimmed[2+closeIdx:]
		body = consumeOptionalSpace(body)

		return types.Identity{Kind: types.IdentityKindSeat, DisplayName: display}, body
	}

	return types.Identity{Kind: types.IdentityKindHuman}, trimmed
}

func consumeOptionalSpace(s string) string {
	if strings.HasPrefix(s, " ") {
		return strings.TrimPrefix(s, " ")
	}

	return s
}
