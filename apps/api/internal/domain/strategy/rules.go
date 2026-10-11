package strategy

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
)

var ErrInvalidRuleSet = errors.New("invalid strategy rule set")

var supportedOperators = map[string]struct{}{
	"eq": {}, "neq": {}, "gt": {}, "gte": {}, "lt": {}, "lte": {}, "in": {},
}

func ValidateRuleSet(raw []byte) (RuleSet, error) {
	var rules RuleSet
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&rules); err != nil {
		return RuleSet{}, fmt.Errorf("%w: %v", ErrInvalidRuleSet, err)
	}
	var trailing any
	if err := decoder.Decode(&trailing); err == nil {
		return RuleSet{}, fmt.Errorf("%w: multiple JSON values", ErrInvalidRuleSet)
	}
	if rules.SchemaVersion != 1 {
		return RuleSet{}, fmt.Errorf("%w: unsupported schema_version", ErrInvalidRuleSet)
	}
	if len(rules.Entry) == 0 || len(rules.Exit) == 0 {
		return RuleSet{}, fmt.Errorf("%w: entry and exit must not be empty", ErrInvalidRuleSet)
	}
	for _, c := range append(append([]Condition{}, rules.Entry...), rules.Exit...) {
		if c.Field == "" {
			return RuleSet{}, fmt.Errorf("%w: condition field is required", ErrInvalidRuleSet)
		}
		if _, ok := supportedOperators[c.Operator]; !ok {
			return RuleSet{}, fmt.Errorf("%w: unsupported operator %q", ErrInvalidRuleSet, c.Operator)
		}
		if len(c.Value) == 0 || bytes.Equal(c.Value, []byte("null")) {
			return RuleSet{}, fmt.Errorf("%w: condition value is required", ErrInvalidRuleSet)
		}
	}
	if rules.Risk.StopDistance <= 0 || rules.Risk.TargetDistance <= 0 || rules.Risk.MaxVolume <= 0 {
		return RuleSet{}, fmt.Errorf("%w: risk values must be positive", ErrInvalidRuleSet)
	}
	return rules, nil
}
