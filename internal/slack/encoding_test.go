package slack

import (
	"testing"

	"github.com/itsHabib/huddle/internal/types"

	"github.com/stretchr/testify/require"
)

func TestDecodeEncodeSeatRoundTrip(t *testing.T) {
	t.Parallel()

	ident := types.Identity{Kind: types.IdentityKindSeat, DisplayName: "alpha"}
	body := `hello [] *world`

	marshaled := Encode(ident, body)
	require.Equal(t, "[alpha] "+body, marshaled)

	gotIdent, gotBody := Decode(marshaled)
	require.Equal(t, ident, gotIdent)
	require.Equal(t, body, gotBody)
}

func TestDecodeEncodeOrchestratorRoundTrip(t *testing.T) {
	t.Parallel()

	ident := types.Identity{Kind: types.IdentityKindOrchestrator, DisplayName: "ops"}
	body := "ship it"

	rendered := Encode(ident, body)

	require.Equal(t, "*[ops] ship it", rendered)

	gotIdent, gotBody := Decode(rendered)

	require.Equal(t, ident, gotIdent)
	require.Equal(t, body, gotBody)
}

func TestHumanPlainText_NoPrefix(t *testing.T) {
	t.Parallel()

	txt := `  slack says hi  `
	id, body := Decode(txt)

	require.Equal(t, types.IdentityKindHuman, id.Kind)

	require.Empty(t, id.DisplayName)

	require.Equal(t, "slack says hi", body)
}

func TestHumanBracketsMiddle(t *testing.T) {
	t.Parallel()

	id, body := Decode("prefix [seat] hello")

	require.Equal(t, types.IdentityKindHuman, id.Kind)

	require.Equal(t, "prefix [seat] hello", body)
}

func TestMalformedUnclosedSeat(t *testing.T) {
	t.Parallel()

	raw := "[oops no close"
	id, preserved := Decode(raw)

	require.Equal(t, types.IdentityKindUnknown, id.Kind)

	require.Equal(t, raw, preserved)
}

func TestMalformedOrchestratorUnclosed(t *testing.T) {
	t.Parallel()

	raw := "*[orphan prefix"
	id, preserved := Decode(raw)

	require.Equal(t, types.IdentityKindUnknown, id.Kind)

	require.Equal(t, raw, preserved)
}

func TestOptionalSpaceAfterCloseBracket(t *testing.T) {
	t.Parallel()

	id, body := Decode("[x]attached")

	require.Equal(t, types.Identity{Kind: types.IdentityKindSeat, DisplayName: "x"}, id)

	require.Equal(t, "attached", body)

	id2, body2 := Decode("*[bot]oops")

	require.Equal(t, types.Identity{Kind: types.IdentityKindOrchestrator, DisplayName: "bot"}, id2)

	require.Equal(t, "oops", body2)
}

func TestEmptyBodyVariants(t *testing.T) {
	t.Parallel()

	o, bb := Decode("*[orchestrator] ")
	require.Equal(t, types.IdentityKindOrchestrator, o.Kind)

	require.Empty(t, bb)

	s, bs := Decode("[seat] ")
	require.Equal(t, types.IdentityKindSeat, s.Kind)

	require.Empty(t, bs)
}

func TestEncodeLeavesUnknownKindsBare(t *testing.T) {
	t.Parallel()

	m := Encode(types.Identity{Kind: types.IdentityKindHuman, DisplayName: "ignored"}, "plain")

	require.Equal(t, "plain", m)
}
