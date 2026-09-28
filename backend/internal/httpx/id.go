package httpx

import (
	"errors"
	"strconv"
	"strings"
)

// ErrInvalidID is the single error returned for every rejected ID, so callers
// cannot distinguish a malformed ID from an out-of-range one.
var ErrInvalidID = errors.New("invalid resource id")

// maxIDDigits is the width of 18446744073709551615, the largest BIGINT UNSIGNED.
const maxIDDigits = 20

// ID is a BIGINT UNSIGNED primary key. It serialises as a JSON string because
// JavaScript numbers cannot represent every uint64 exactly.
type ID uint64

// MarshalJSON renders the ID as a decimal string.
func (id ID) MarshalJSON() ([]byte, error) {
	return []byte(strconv.Quote(strconv.FormatUint(uint64(id), 10))), nil
}

// UnmarshalJSON accepts only a decimal string, matching what MarshalJSON emits.
func (id *ID) UnmarshalJSON(data []byte) error {
	raw, err := strconv.Unquote(string(data))
	if err != nil {
		return ErrInvalidID
	}

	parsed, err := ParseID(raw)
	if err != nil {
		return err
	}

	*id = parsed
	return nil
}

// ParseID accepts the values a path segment may hold: an ASCII decimal string
// within 1..18446744073709551615, with leading zeros permitted. Everything
// else — empty, spaces, signs, "0", fractions, exponents, non-ASCII digits and
// overflow — yields ErrInvalidID.
func ParseID(raw string) (ID, error) {
	if raw == "" {
		return 0, ErrInvalidID
	}

	for i := 0; i < len(raw); i++ {
		if raw[i] < '0' || raw[i] > '9' {
			return 0, ErrInvalidID
		}
	}

	// Leading zeros carry no value, so "001" and "0000...001" are both 1.
	// Stripping them first means the digit cap only rejects real overflow.
	significand := strings.TrimLeft(raw, "0")
	if significand == "" || len(significand) > maxIDDigits {
		return 0, ErrInvalidID
	}

	value, err := strconv.ParseUint(significand, 10, 64)
	if err != nil {
		return 0, ErrInvalidID
	}

	return ID(value), nil
}

// String returns the decimal form used in URLs and JSON.
func (id ID) String() string {
	return strconv.FormatUint(uint64(id), 10)
}
