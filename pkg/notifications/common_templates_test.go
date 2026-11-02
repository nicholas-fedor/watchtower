package notifications

import (
	"go/ast"
	"go/constant"
	"go/parser"
	"go/token"
	"go/types"
	"regexp"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// loggedMessagesFile is the source file that declares the log messages the
// built-in templates match, relative to this package.
const loggedMessagesFile = "../../internal/observability/logging/messages.go"

// templateMessageCase matches a template case on a log message, such as
// eq $msg "Found new image". It captures the message.
var templateMessageCase = regexp.MustCompile(`eq \$msg "([^"]*)"`)

// TestCommonTemplates_MatchLoggedMessages verifies that the built-in templates
// match exactly the messages declared in the logging package. Templates select
// a case by the text of a log message. The code that logs a message uses its
// declared constant. A template case without a constant would never match a
// logged message. A constant without a template case would format only
// generically. The golden fixtures must cover the same messages.
func TestCommonTemplates_MatchLoggedMessages(t *testing.T) {
	t.Parallel()

	var matched []string

	seen := map[string]bool{}

	for _, tpl := range commonTemplates {
		for _, match := range templateMessageCase.FindAllStringSubmatch(tpl, -1) {
			if !seen[match[1]] {
				seen[match[1]] = true
				matched = append(matched, match[1])
			}
		}
	}

	logged := loggedMessages(t)

	assert.ElementsMatch(t, logged, matched)
	assert.ElementsMatch(t, logged, legacyTemplateMessages,
		"the golden fixtures cover every message the templates match")
}

// loggedMessages returns the value of every constant declared in the logging
// package's message file. It fails the test when a constant there is not a
// string.
func loggedMessages(t *testing.T) []string {
	t.Helper()

	fset := token.NewFileSet()

	file, err := parser.ParseFile(fset, loggedMessagesFile, nil, 0)
	require.NoError(t, err)

	pkg, err := new(types.Config).Check("logging", fset, []*ast.File{file}, nil)
	require.NoError(t, err)

	var messages []string

	for _, name := range pkg.Scope().Names() {
		declared, ok := pkg.Scope().Lookup(name).(*types.Const)
		if !ok {
			continue
		}

		require.Equal(t, constant.String, declared.Val().Kind(), "constant %s is not a message", name)

		messages = append(messages, constant.StringVal(declared.Val()))
	}

	return messages
}
