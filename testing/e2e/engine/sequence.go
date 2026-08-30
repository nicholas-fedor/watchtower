package engine

import (
	"fmt"
	"iter"
)

const (
	// generatorProduct is the cartesian Product iterator.
	generatorProduct = "product"
	// generatorRandom is the seeded Random iterator.
	generatorRandom = "random"
	// generatorFile loads YAML cases from disk.
	generatorFile = "file"
)

// SequenceRequest selects which case iterator a sitting consumes.
type SequenceRequest struct {
	// Generator is product, random, or file.
	Generator string
	// Seed is the random generator seed.
	Seed int64
	// FilePath is the YAML path when Generator is file.
	FilePath string
}

// Sequence returns the case iterator for a sitting generator.
//
// Parameters:
//   - req: Generator selection.
//
// Returns:
//   - iter.Seq[Case]: Case stream.
//   - error: Missing file path or YAML load failure.
func Sequence(req SequenceRequest) (iter.Seq[Case], error) {
	switch req.Generator {
	case generatorRandom:
		return Random(Model(), req.Seed), nil
	case generatorFile:
		if req.FilePath == "" {
			return nil, ErrFileGeneratorNeedsPath
		}

		cases, err := LoadFile(req.FilePath)
		if err != nil {
			return nil, fmt.Errorf("load file cases: %w", err)
		}

		return CasesFromSlice(cases), nil
	case generatorProduct:
		return Product(Model()), nil
	default:
		return Product(Model()), nil
	}
}

// WorkBound is how many cases this sitting will run when that number is known.
//
// File generators use the YAML length. Product and random are unbounded unless
// limit is set. Zero means "unknown / unbounded".
//
// Parameters:
//   - generator: product, random, or file.
//   - filePath: YAML path when generator is file.
//   - limit: --limit. Zero means no cap.
//
// Returns:
//   - int: Known job count, or 0 if unbounded.
//   - error: File load failure.
func WorkBound(generator, filePath string, limit int) (int, error) {
	known := 0

	if generator == generatorFile {
		if filePath == "" {
			return 0, ErrFileGeneratorNeedsPath
		}

		cases, err := LoadFile(filePath)
		if err != nil {
			return 0, err
		}

		known = len(cases)
	}

	if limit > 0 {
		if known == 0 {
			return limit, nil
		}

		return min(known, limit), nil
	}

	return known, nil
}
