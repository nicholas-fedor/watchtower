package engine

import "strings"

// FactorRow is one Model() axis for list --dump-factors.
type FactorRow struct {
	// Name is the factor key.
	Name string
	// Count is the number of levels.
	Count int
	// Levels is a comma-joined level list.
	Levels string
}

// Inventory is the no-Docker product summary printed by e2e list.
type Inventory struct {
	// Generator is the requested generator name.
	Generator string
	// Cardinality is the product size as a decimal string.
	Cardinality string
	// FactorCount is len(Model()).
	FactorCount int
	// FirstID is the first product case ID.
	FirstID string
	// LastID is the last product case ID.
	LastID string
	// Factors is the dump-factors table. Nil when dump is false.
	Factors []FactorRow
}

// BuildInventory computes cardinality, first/last IDs, and optional factor rows.
//
// Parameters:
//   - generator: Label included in the summary.
//   - dump: When true, include every factor name and level list.
//
// Returns:
//   - Inventory: List command payload.
func BuildInventory(generator string, dump bool) Inventory {
	factors := Model()
	inv := Inventory{
		Generator:   generator,
		Cardinality: Cardinality(factors).String(),
		FactorCount: len(factors),
	}

	first, ok := First(factors)
	if ok {
		inv.FirstID = first.ID()
	}

	last, lastOK := Last(factors)
	if lastOK {
		inv.LastID = last.ID()
	}

	if !dump {
		return inv
	}

	inv.Factors = make([]FactorRow, 0, len(factors))
	for _, factor := range factors {
		inv.Factors = append(inv.Factors, FactorRow{
			Name:   factor.Name,
			Count:  len(factor.Levels),
			Levels: strings.Join(factor.Levels, ","),
		})
	}

	return inv
}
