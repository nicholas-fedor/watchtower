package main

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestClassifyJSType(t *testing.T) {
	t.Parallel()

	tests := []struct {
		give string
		want jsValueKind
	}{
		{give: "string", want: jsKindString},
		{give: "object", want: jsKindCollection},
		{give: "null", want: jsKindInvalid},
		{give: "undefined", want: jsKindInvalid},
		{give: "number", want: jsKindInvalid},
		{give: "boolean", want: jsKindInvalid},
		{give: "symbol", want: jsKindInvalid},
		{give: "function", want: jsKindInvalid},
		{give: "", want: jsKindInvalid},
	}

	for _, tt := range tests {
		t.Run(tt.give, func(t *testing.T) {
			t.Parallel()

			assert.Equal(t, tt.want, classifyJSType(tt.give))
		})
	}
}
