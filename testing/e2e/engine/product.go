package engine

import (
	"iter"
	"math/big"
)

// Product streams the cartesian product of factor levels. It never materializes
// the full space. Each yielded case has every factor assigned and a stable ID.
//
// Parameters:
//   - factors: Cartesian axes. Empty input yields nothing.
//
// Returns:
//   - iter.Seq[Case]: Pull iterator over complete vectors.
func Product(factors []Factor) iter.Seq[Case] {
	return func(yield func(Case) bool) {
		if len(factors) == 0 {
			return
		}

		for _, factor := range factors {
			if len(factor.Levels) == 0 {
				return
			}
		}

		indices := make([]int, len(factors))
		for {
			item := applyIndices(factors, indices)
			if !yield(item) {
				return
			}

			if !incrementIndices(indices, factors) {
				return
			}
		}
	}
}

// applyIndices builds one case from the current mixed-radix index vector.
//
// Parameters:
//   - factors: Cartesian axes.
//   - indices: Level index per factor.
//
// Returns:
//   - Case: Fully applied vector with derived Expect and ID.
func applyIndices(factors []Factor, indices []int) Case {
	item := Case{
		Factors: make(map[string]string, len(factors)),
	}

	for idx, factor := range factors {
		level := factor.Levels[indices[idx]]

		item.Factors[factor.Name] = level
		if factor.Apply != nil {
			factor.Apply(&item, level)
		}
	}

	item.Expect = DeriveExpect(item)
	item.AssignID()

	return item
}

// incrementIndices advances mixed-radix indices. False means the product is exhausted.
//
// Parameters:
//   - indices: Current indices, mutated in place.
//   - factors: Axes whose level counts are the radices.
//
// Returns:
//   - bool: True when another combination remains.
func incrementIndices(indices []int, factors []Factor) bool {
	for idx := len(indices) - 1; idx >= 0; idx-- {
		indices[idx]++
		if indices[idx] < len(factors[idx].Levels) {
			return true
		}

		indices[idx] = 0
	}

	return false
}

// Cardinality is the product of level counts. It does not iterate the space.
//
// Parameters:
//   - factors: Cartesian axes.
//
// Returns:
//   - *big.Int: Number of combinations. Zero when any factor has no levels.
func Cardinality(factors []Factor) *big.Int {
	total := big.NewInt(1)

	for _, factor := range factors {
		if len(factor.Levels) == 0 {
			return big.NewInt(0)
		}

		total.Mul(total, big.NewInt(int64(len(factor.Levels))))
	}

	return total
}

// First yields the first product case (every factor at level 0).
//
// Parameters:
//   - factors: Cartesian axes.
//
// Returns:
//   - Case: First vector.
//   - bool: False when the product is empty.
func First(factors []Factor) (Case, bool) {
	for item := range Product(factors) {
		return item, true
	}

	return Case{}, false
}

// Last yields the last product case (every factor at its final level).
//
// Parameters:
//   - factors: Cartesian axes.
//
// Returns:
//   - Case: Last vector.
//   - bool: False when the product is empty.
func Last(factors []Factor) (Case, bool) {
	if len(factors) == 0 {
		return Case{}, false
	}

	indices := make([]int, len(factors))
	for idx, factor := range factors {
		if len(factor.Levels) == 0 {
			return Case{}, false
		}

		indices[idx] = len(factor.Levels) - 1
	}

	return applyIndices(factors, indices), true
}
