package xun

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/yaoapp/xun/dbal"
	"github.com/yaoapp/xun/dbal/schema"
)

func TestCleanDefaultString(t *testing.T) {
	assert.Equal(t, "0.00", cleanDefaultString("'0.00'"))
	assert.Equal(t, "0.00", cleanDefaultString(" '0.00' \r\n"))
	assert.Equal(t, "pending", cleanDefaultString("'pending'"))
	assert.Equal(t, "hello'world", cleanDefaultString("'hello''world'"))
	assert.Equal(t, "", cleanDefaultString("''"))
	assert.Equal(t, "0", cleanDefaultString("0"))
}

func TestColumnToBlueprintQuotedDefault(t *testing.T) {
	// Decimal column with '0.00' default
	defVal := "'0.00'"
	col := &schema.Column{
		Column: &dbal.Column{
			Name:    "number",
			Type:    "decimal",
			Default: &defVal,
		},
	}

	bpCol := ColumnToBlueprint(col, []string{})
	assert.Equal(t, "number", bpCol.Name)
	assert.Equal(t, "decimal", bpCol.Type)
	assert.Equal(t, float64(0), bpCol.Default)

	// Integer column with '1' default
	defInt := "'1'"
	colInt := &schema.Column{
		Column: &dbal.Column{
			Name:    "status",
			Type:    "integer",
			Default: &defInt,
		},
	}

	bpColInt := ColumnToBlueprint(colInt, []string{})
	assert.Equal(t, 1, bpColInt.Default)

	// String column with 'pending' default
	defStr := "'pending'"
	colStr := &schema.Column{
		Column: &dbal.Column{
			Name:    "status_str",
			Type:    "string",
			Default: &defStr,
		},
	}

	bpColStr := ColumnToBlueprint(colStr, []string{})
	assert.Equal(t, "pending", bpColStr.Default)
}
