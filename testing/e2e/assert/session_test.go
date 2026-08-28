package assert

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestAssertDependencyOrder(t *testing.T) {
	t.Parallel()

	logs := "" +
		`{"message":"Stopping container","container":"e2e-d"}` + "\n" +
		`{"message":"Stopping container","container":"e2e-c"}` + "\n" +
		`{"message":"Stopping container","container":"e2e-b"}` + "\n" +
		`{"message":"Stopping container","container":"e2e-a"}` + "\n" +
		`{"message":"Started new container","container":"e2e-a"}` + "\n" +
		`{"message":"Started new container","container":"e2e-b"}` + "\n" +
		`{"message":"Started new container","container":"e2e-c"}` + "\n" +
		`{"message":"Started new container","container":"e2e-d"}` + "\n"

	events := ParseSession(logs)
	require.NoError(t, AssertDependencyOrder(events, []string{"e2e-a", "e2e-b", "e2e-c", "e2e-d"}))
}

func TestAssertDependencyOrderRejectsWrongStop(t *testing.T) {
	t.Parallel()

	logs := "" +
		`{"message":"Stopping container","container":"e2e-a"}` + "\n" +
		`{"message":"Stopping container","container":"e2e-b"}` + "\n" +
		`{"message":"Started new container","container":"e2e-a"}` + "\n" +
		`{"message":"Started new container","container":"e2e-b"}` + "\n"

	events := ParseSession(logs)
	err := AssertDependencyOrder(events, []string{"e2e-a", "e2e-b"})
	require.ErrorIs(t, err, ErrDependencyOrder)
}
