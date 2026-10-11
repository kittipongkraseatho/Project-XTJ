package strategy

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"
)

func validRulesJSON() []byte {
	return []byte(`{
        "schema_version": 1,
        "setup": {"symbols": ["EURUSD"], "directions": ["long"]},
        "entry": [{"field": "rsi", "operator": "lt", "value": 30}],
        "exit": [{"field": "rsi", "operator": "gt", "value": 70}],
        "risk": {"stop_distance": 10, "target_distance": 20, "max_volume": 1}
    }`)
}

func TestValidateRuleSetAcceptsVersionOneRules(t *testing.T) {
	rules, err := ValidateRuleSet(validRulesJSON())

	require.NoError(t, err)
	require.Equal(t, 1, rules.SchemaVersion)
	require.Equal(t, []string{"EURUSD"}, rules.Setup.Symbols)
	require.Len(t, rules.Entry, 1)
}

func TestValidateRuleSetRejectsUnknownOperator(t *testing.T) {
	raw := map[string]any{}
	require.NoError(t, json.Unmarshal(validRulesJSON(), &raw))
	raw["entry"] = []any{map[string]any{"field": "rsi", "operator": "between", "value": 30}}
	body, err := json.Marshal(raw)
	require.NoError(t, err)

	_, err = ValidateRuleSet(body)

	require.ErrorIs(t, err, ErrInvalidRuleSet)
}

func TestValidateRuleSetRejectsNonPositiveRisk(t *testing.T) {
	raw := map[string]any{}
	require.NoError(t, json.Unmarshal(validRulesJSON(), &raw))
	raw["risk"] = map[string]any{"stop_distance": 0, "target_distance": 20, "max_volume": 1}
	body, err := json.Marshal(raw)
	require.NoError(t, err)

	_, err = ValidateRuleSet(body)

	require.ErrorIs(t, err, ErrInvalidRuleSet)
}

func TestValidateRuleSetRejectsUnknownFields(t *testing.T) {
	raw := map[string]any{}
	require.NoError(t, json.Unmarshal(validRulesJSON(), &raw))
	raw["unexpected"] = true
	body, err := json.Marshal(raw)
	require.NoError(t, err)

	_, err = ValidateRuleSet(body)

	require.ErrorIs(t, err, ErrInvalidRuleSet)
}
