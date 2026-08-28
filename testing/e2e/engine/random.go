package engine

import (
	"iter"
	"math/rand/v2"
)

// Random streams independent full-vector draws forever. The caller stops the
// iterator. The same seed reproduces the same sequence.
//
// Parameters:
//   - factors: Axes to sample.
//   - seed: RNG seed.
//
// Returns:
//   - iter.Seq[Case]: Infinite iterator until the consumer stops ranging.
func Random(factors []Factor, seed int64) iter.Seq[Case] {
	return func(yield func(Case) bool) {
		if len(factors) == 0 {
			return
		}

		rng := rand.New(rand.NewPCG(uint64(seed), uint64(seed)^pcgInc)) //nolint:gosec // G404: reproducible case sampling, not cryptography.

		for {
			item := Case{
				Factors: make(map[string]string, len(factors)),
			}
			for _, factor := range factors {
				if len(factor.Levels) == 0 {
					return
				}

				level := factor.Levels[rng.IntN(len(factor.Levels))]

				item.Factors[factor.Name] = level
				if factor.Apply != nil {
					factor.Apply(&item, level)
				}
			}

			item.Expect = DeriveExpect(item)
			item.AssignID()

			if !yield(item) {
				return
			}
		}
	}
}

// pcgInc is the second PCG stream constant mixed into the seed.
const pcgInc = 0x9e3779b97f4a7c15
