package history

import (
	"fmt"
	"strconv"
	"time"
)

func parseTimeParam(value string) (*time.Time, error) {
	var noTime *time.Time
	if value == "" {
		return noTime, errNoTimeParameter
	}

	t, err := time.Parse(time.RFC3339, value)
	if err != nil {
		return nil, fmt.Errorf("%w: %w", errInvalidTimeParameter, err)
	}

	return &t, nil
}

func parseLimit(value string) (int, error) {
	if value == "" {
		return 0, nil
	}

	limit, err := strconv.Atoi(value)
	if err != nil || limit < 0 {
		return 0, fmt.Errorf("expected non-negative integer: %w", err)
	}

	return limit, nil
}
