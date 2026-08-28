package engine

import (
	"fmt"
	"strconv"
	"strings"
)

// shardParts is the number of components in an i/n shard spec.
const shardParts = 2

// ParseShard decodes an i/n shard spec. Empty input means no sharding.
//
// Parameters:
//   - spec: Shard string such as "2/8". Empty means all shards.
//
// Returns:
//   - int: 1-based shard index.
//   - int: Shard total.
//   - error: Syntax or range error.
func ParseShard(spec string) (int, int, error) {
	if spec == "" {
		return 0, 0, nil
	}

	parts := strings.Split(spec, "/")
	if len(parts) != shardParts {
		return 0, 0, ErrShardSyntax
	}

	index, err := strconv.Atoi(parts[0])
	if err != nil {
		return 0, 0, fmt.Errorf("shard index: %w", err)
	}

	total, totalErr := strconv.Atoi(parts[1])
	if totalErr != nil {
		return 0, 0, fmt.Errorf("shard total: %w", totalErr)
	}

	if index < 1 || total < 1 || index > total {
		return 0, 0, fmt.Errorf("%w: %s", ErrShardRange, spec)
	}

	return index, total, nil
}
