package units

import (
	"errors"
	"fmt"
	"math"
	"math/big"
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

	bytes, err := scaleDiskSpaceValue(valueStr, multiplier)
	if err != nil {
		return 0, fmt.Errorf("invalid disk space %q: %w", size, err)
	}

	return bytes, nil
}

// ParseDiskSpacePercent parses the number of a disk-space percentage, such as
// the "80" of "80%".
//
// It accepts the same form as the number of a size, digits with at most one
// decimal point, so NaN, infinities, signs, and exponents are rejected. The
// caller checks the range.
//
// Parameters:
//   - value: Percentage number without the "%" sign.
//
// Returns:
//   - float64: Parsed percentage.
//   - error: Non-nil if the number is not in the accepted form.
func ParseDiskSpacePercent(value string) (float64, error) {
	value = strings.TrimSpace(value)
	if !isDiskSpaceNumber(value) {
		return 0, fmt.Errorf("invalid percentage %q: %w", value, errInvalidDiskSpaceNumber)
	}

	percent, err := strconv.ParseFloat(value, 64)
	if err != nil {
		return 0, fmt.Errorf("invalid percentage %q: %w", value, errInvalidDiskSpaceNumber)
	}

	return percent, nil
}

// scaleDiskSpaceValue converts a numeric size string into bytes using multiplier.
//
// The value must be digits with at most one decimal point. Integer values use
// overflow-checked integer arithmetic. Fractional values use exact rational
// arithmetic and truncate toward zero to whole bytes.
//
// Parameters:
//   - valueStr: Numeric portion of the size string.
//   - multiplier: Unit multiplier in bytes.
//
// Returns:
//   - int64: Size in bytes.
//   - error: Non-nil if the number is invalid, negative, or overflows int64.
func scaleDiskSpaceValue(valueStr string, multiplier int64) (int64, error) {
	valueStr = strings.TrimSpace(valueStr)
	if !isDiskSpaceNumber(valueStr) {
		return 0, errInvalidDiskSpaceNumber
	}

	if strings.Contains(valueStr, ".") {
		return scaleDecimalDiskSpace(valueStr, multiplier)
	}

	return scaleIntegerDiskSpace(valueStr, multiplier)
}

// isDiskSpaceNumber reports whether valueStr is digits with at most one decimal
// point.
//
// The integer and rational parsers also accept signs, exponents, fractions,
// and hexadecimal floats, which the size format does not allow.
//
// Parameters:
//   - valueStr: Numeric portion of the size string.
//
// Returns:
//   - bool: True when valueStr has at least one digit, at most one decimal
//     point, and nothing else.
func isDiskSpaceNumber(valueStr string) bool {
	digits, points := 0, 0

	for _, r := range valueStr {
		switch {
		case r >= '0' && r <= '9':
			digits++
		case r == '.':
			points++
		default:
			return false
		}
	}

	return digits > 0 && points <= 1
}

// scaleIntegerDiskSpace parses a whole-number size and multiplies it by the unit.
//
// Parameters:
//   - valueStr: Integer numeric portion.
//   - multiplier: Unit multiplier in bytes.
//
// Returns:
//   - int64: Size in bytes.
//   - error: Non-nil if the number is invalid, negative, or overflows int64.
func scaleIntegerDiskSpace(valueStr string, multiplier int64) (int64, error) {
	value, err := strconv.ParseInt(valueStr, 10, 64)
	if err != nil {
		var numErr *strconv.NumError
		if errors.As(err, &numErr) && errors.Is(numErr.Err, strconv.ErrRange) {
			return 0, errDiskSpaceOverflow
		}

		return 0, errInvalidDiskSpaceNumber
	}

	if value < 0 {
		return 0, errNegativeDiskSpace
	}

	product := new(big.Int).Mul(big.NewInt(value), big.NewInt(multiplier))
	if !product.IsInt64() {
		return 0, errDiskSpaceOverflow
	}

	return product.Int64(), nil
}

// scaleDecimalDiskSpace parses a fractional size with exact rational arithmetic.
//
// Parameters:
//   - valueStr: Decimal numeric portion.
//   - multiplier: Unit multiplier in bytes.
//
// Returns:
//   - int64: Size in bytes, truncated toward zero.
//   - error: Non-nil if the number is invalid, negative, or overflows int64.
func scaleDecimalDiskSpace(valueStr string, multiplier int64) (int64, error) {
	rat, ok := new(big.Rat).SetString(valueStr)
	if !ok {
		return 0, errInvalidDiskSpaceNumber
	}

	if rat.Sign() < 0 {
		return 0, errNegativeDiskSpace
	}

	rat.Mul(rat, new(big.Rat).SetInt64(multiplier))

	bytes := new(big.Int).Quo(rat.Num(), rat.Denom())

	if !bytes.IsInt64() {
		return 0, errDiskSpaceOverflow
	}

	return bytes.Int64(), nil
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

// FormatDiskSpace formats a byte count as a compact decimal size string.
//
// Values below 1000 bytes stay in bytes. Larger values use KB, MB, GB, TB, or PB
// (powers of 1000) to match ParseDiskSpace decimal units. Exact multiples are
// whole numbers. Other values use up to two decimal places with trailing zeros
// trimmed. A value that rounds to 1000 of its unit uses the next larger unit,
// so 999,999 bytes is "1 MB" rather than "1000 KB".
//
// Parameters:
//   - bytes: Size in bytes. Negative values are formatted with a leading minus.
//
// Returns:
//   - string: Human-readable size such as "10 GB" or "512 B".
func FormatDiskSpace(bytes int64) string {
	if bytes == math.MinInt64 {
		return "-" + FormatDiskSpace(math.MaxInt64)
	}

	if bytes < 0 {
		return "-" + FormatDiskSpace(-bytes)
	}

	units := []struct {
		suffix string
		size   int64
	}{
		{"PB", petabyteMultiplier},
		{"TB", terabyteMultiplier},
		{"GB", gigabyteMultiplier},
		{"MB", megabyteMultiplier},
		{"KB", kilobyteMultiplier},
	}

	// larger is the unit checked before the current one, which is the next
	// larger unit.
	larger, hasLarger := units[0], false

	for _, unit := range units {
		if bytes < unit.size {
			larger, hasLarger = unit, true

			continue
		}

		quantity := formatDiskSpaceQuantity(bytes, unit.size)

		// A value below 1000 of its unit can round up to exactly 1000, which
		// the next larger unit shows as 1.
		if quantity == "1000" && hasLarger {
			unit = larger
			quantity = formatDiskSpaceQuantity(bytes, unit.size)
		}

		return quantity + " " + unit.suffix
	}

	return strconv.FormatInt(bytes, 10) + " B"
}

// formatDiskSpaceQuantity formats bytes as a decimal quantity of the given unit.
//
// Parameters:
//   - bytes: Size in bytes. Must be non-negative.
//   - size: Unit multiplier in bytes.
//
// Returns:
//   - string: Quantity with up to two decimal places and no trailing zeros.
func formatDiskSpaceQuantity(bytes, size int64) string {
	if bytes%size == 0 {
		return strconv.FormatInt(bytes/size, 10)
	}

	formatted := strconv.FormatFloat(float64(bytes)/float64(size), 'f', 2, 64)
	formatted = strings.TrimRight(formatted, "0")

	return strings.TrimRight(formatted, ".")
}
