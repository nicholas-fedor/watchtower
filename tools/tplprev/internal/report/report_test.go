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
// and container report methods in the preview as in Watchtower. The expected
// lists are generated from Watchtower's report types by `task tplprev:gen`.
func TestInterfacesMatchWatchtower(t *testing.T) {
	t.Parallel()

	tests := []struct {
		// interfaceType is the preview interface whose methods are checked.
		interfaceType reflect.Type
		// name names the subtest.
		name string
		// contract is the generated list of Watchtower's method names.
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

			got := make([]string, 0, tt.interfaceType.NumMethod())
			for method := range tt.interfaceType.Methods() {
				got = append(got, method.Name)
			}

			slices.Sort(got)

			assert.Equal(t, want, got)
		})
	}
}
