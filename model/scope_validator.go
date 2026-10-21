package model

import (
	"encoding/json"
	"fmt"
	"regexp"
)

var validIdentifierRegex = regexp.MustCompile(`^[a-zA-Z_][a-zA-Z0-9_]*$`)

// CompileAndValidateCondition 校验并编译自定义数据权限规则，防 SQL 注入与字段逃逸
func CompileAndValidateCondition(mod *Model, rawJSON string) (*ConditionValueSchema, error) {
	if rawJSON == "" {
		return nil, nil
	}

	var schema ConditionValueSchema
	if err := json.Unmarshal([]byte(rawJSON), &schema); err != nil {
		return nil, fmt.Errorf("malformed condition_value JSON: %w", err)
	}

	if schema.Logic != "" && schema.Logic != "AND" && schema.Logic != "OR" {
		return nil, fmt.Errorf("invalid logic operator: %s (must be AND or OR)", schema.Logic)
	}

	for _, rule := range schema.Rules {
		if !validIdentifierRegex.MatchString(rule.Field) {
			return nil, fmt.Errorf("security violation: invalid column name [%s]", rule.Field)
		}
		if mod != nil && mod.Columns != nil {
			if _, exists := mod.Columns[rule.Field]; !exists {
				return nil, fmt.Errorf("security violation: column [%s] not found in model [%s]", rule.Field, mod.ID)
			}
		}
		if !isValidSlot(rule.Slot) {
			return nil, fmt.Errorf("security violation: unknown context slot [%s]", rule.Slot)
		}
	}

	return &schema, nil
}

func isValidSlot(slot string) bool {
	switch slot {
	case "", "__user_id", "__dept_id", "__dept_ids", "__org_id", "__tenant_id":
		return true
	default:
		return false
	}
}
