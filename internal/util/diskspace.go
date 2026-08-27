package util

import (
	"errors"
	"fmt"
	"math"
	"strconv"
	"strings"
	"unicode"
)

// Disk space unit multipliers.
const (
	kibibyteMultiplier int64 = 1024
	mebibyteMultiplier       = 1024 * kibibyteMultiplier
	gibibyteMultiplier       = 1024 * mebibyteMultiplier
	tebibyteMultiplier       = 1024 * gibibyteMultiplier
	pebibyteMultiplier       = 1024 * tebibyteMultiplier

	kilobyteMultiplier int64 = 1000
	megabyteMultiplier       = 1000 * kilobyteMultiplier
	gigabyteMultiplier       = 1000 * megabyteMultiplier
	terabyteMultiplier       = 1000 * gigabyteMultiplier
	petabyteMultiplier       = 1000 * terabyteMultiplier
)

var (
	// errNegativeDiskSpace indicates a negative disk-space value was provided.
	errNegativeDiskSpace = errors.New("negative values are not supported")

	// errInvalidDiskSpaceUnit indicates an unrecognized size unit suffix.
	errInvalidDiskSpaceUnit = errors.New("invalid unit in disk space string")

	// errInvalidDiskSpaceNumber indicates the numeric portion could not be parsed.
	errInvalidDiskSpaceNumber = errors.New("invalid numeric value in disk space string")

	// errDiskSpaceOverflow indicates the parsed size exceeds the int64 range.
	errDiskSpaceOverflow = errors.New("disk space value overflows int64")
)

// diskSpaceUnits maps a normalized (lowercase) unit suffix to a byte multiplier.
// Bare integers use multiplier 1 (bytes). Decimal units are powers of 1000;
// binary units (KiB, MiB, ...) are powers of 1024.
var diskSpaceUnits = map[string]int64{
	"":    1,
	"b":   1,
	"k":   kilobyteMultiplier,
	"kb":  kilobyteMultiplier,
	"m":   megabyteMultiplier,
	"mb":  megabyteMultiplier,
	"g":   gigabyteMultiplier,
	"gb":  gigabyteMultiplier,
	"t":   terabyteMultiplier,
	"tb":  terabyteMultiplier,
	"p":   petabyteMultiplier,
	"pb":  petabyteMultiplier,
	"kib": kibibyteMultiplier,
	"mib": mebibyteMultiplier,
	"gib": gibibyteMultiplier,
	"tib": tebibyteMultiplier,
	"pib": pebibyteMultiplier,
}

// ParseDiskSpace parses an absolute disk-space string into bytes.
//
// Supported forms:
//   - empty or "0": zero bytes
//   - bare integer: bytes
//   - decimal units: B, K/KB, M/MB, G/GB, T/TB, P/PB (powers of 1000)
//   - binary units: KiB, MiB, GiB, TiB, PiB (powers of 1024)
//
// Units are case-insensitive. Percentage values are not accepted; resolve those
// against a maximum in the caller.
//
// Parameters:
//   - size: Disk-space string to parse.
//
// Returns:
//   - int64: Size in bytes.
//   - error: Non-nil if the string cannot be parsed.
func ParseDiskSpace(size string) (int64, error) {
	size = strings.TrimSpace(size)
	if size == "" || size == "0" {
		return 0, nil
	}

	if size[0] == '-' {
		return 0, fmt.Errorf("invalid disk space %q: %w", size, errNegativeDiskSpace)
	}

	valueStr, unit := splitDiskSpaceValueAndUnit(size)

	multiplier, ok := diskSpaceUnits[strings.ToLower(unit)]
	if !ok {
		return 0, fmt.Errorf("invalid disk space %q: %w", size, errInvalidDiskSpaceUnit)
	}

	value, err := strconv.ParseFloat(valueStr, 64)
	if err != nil {
		return 0, fmt.Errorf("invalid disk space %q: %w", size, errInvalidDiskSpaceNumber)
	}

	if value < 0 {
		return 0, fmt.Errorf("invalid disk space %q: %w", size, errNegativeDiskSpace)
	}

	if math.IsNaN(value) || math.IsInf(value, 0) {
		return 0, fmt.Errorf("invalid disk space %q: %w", size, errInvalidDiskSpaceNumber)
	}

	product := value * float64(multiplier)
	if product > float64(math.MaxInt64) {
		return 0, fmt.Errorf("invalid disk space %q: %w", size, errDiskSpaceOverflow)
	}

	return int64(product), nil
}

// splitDiskSpaceValueAndUnit splits a size string into its numeric prefix and
// trailing unit letters. Whitespace between the number and unit is ignored.
//
// Parameters:
//   - size: Trimmed disk-space string with no leading sign.
//
// Returns:
//   - string: Numeric prefix.
//   - string: Unit suffix (possibly empty).
func splitDiskSpaceValueAndUnit(size string) (string, string) {
	end := len(size)
	for end > 0 {
		r := rune(size[end-1])
		if unicode.IsLetter(r) {
			end--

			continue
		}

		break
	}

	return strings.TrimSpace(size[:end]), size[end:]
}
