package report

import (
	"os"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestInterfacesMatchWatchtower checks that templates can call the same report
// and container report methods in the preview as in Watchtower, with the same
// parameter and result types. The expected lists are generated from
// Watchtower's report types by `task tplprev:gen`.
func TestInterfacesMatchWatchtower(t *testing.T) {
	t.Parallel()

	tests := []struct {
		// interfaceType is the preview interface whose methods are checked.
		interfaceType reflect.Type
		// name names the subtest.
		name string
		// contract is the generated list of Watchtower's method signatures.
		contract string
	}{
		{
			name:          "report",
			contract:      "testdata/report-methods.txt",
			interfaceType: reflect.TypeFor[Report](),
		},
		{
			name:          "container report",
			contract:      "testdata/container-report-methods.txt",
			interfaceType: reflect.TypeFor[ContainerReport](),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			contents, err := os.ReadFile(tt.contract)
			require.NoError(t, err)

			var want []string

			for line := range strings.Lines(string(contents)) {
				line = strings.TrimSpace(line)
				if line != "" && !strings.HasPrefix(line, "#") {
					want = append(want, line)
				}
			}

			assert.Equal(t, want, methodSignatures(tt.interfaceType))
		})
	}
}

// methodSignatures returns the sorted signatures of an interface's methods,
// such as "Focus() ContainerReport". It writes them as Watchtower's generator
// does. The generated lists then compare directly.
//
// Parameters:
//   - interfaceType: The interface type.
//
// Returns:
//   - []string: The method signatures, sorted.
func methodSignatures(interfaceType reflect.Type) []string {
	signatures := make([]string, 0, interfaceType.NumMethod())

	for method := range interfaceType.Methods() {
		params := make([]string, 0, method.Type.NumIn())
		for param := range method.Type.Ins() {
			params = append(params, typeName(param))
		}

		results := make([]string, 0, method.Type.NumOut())
		for result := range method.Type.Outs() {
			results = append(results, typeName(result))
		}

		signature := method.Name + "(" + strings.Join(params, ", ") + ")"

		switch len(results) {
		case 0:
		case 1:
			signature += " " + results[0]
		default:
			signature += " (" + strings.Join(results, ", ") + ")"
		}

		signatures = append(signatures, signature)
	}

	slices.Sort(signatures)

	return signatures
}

// typeName returns a type's name without its package name, such as
// "[]ContainerReport".
//
// Parameters:
//   - valueType: The type to name.
//
// Returns:
//   - string: The type's name.
func typeName(valueType reflect.Type) string {
	switch valueType.Kind() {
	case reflect.Slice:
		return "[]" + typeName(valueType.Elem())
	case reflect.Pointer:
		return "*" + typeName(valueType.Elem())
	default:
		if name := valueType.Name(); name != "" {
			return name
		}

		return valueType.String()
	}
}
