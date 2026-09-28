package httpx

import (
	"encoding/json"
	"errors"
	"testing"
)

func TestParseIDAcceptsDecimalInRange(t *testing.T) {
	cases := map[string]ID{
		"1":                      1,
		"001":                    1,
		"0000000000000000000001": 1,
		"42":                     42,
		"18446744073709551615":   ID(18446744073709551615),
	}

	for raw, want := range cases {
		got, err := ParseID(raw)
		if err != nil {
			t.Errorf("ParseID(%q) returned error %v, want %d", raw, err, want)
			continue
		}
		if got != want {
			t.Errorf("ParseID(%q) = %d, want %d", raw, got, want)
		}
	}
}

func TestParseIDRejectsEveryOtherShape(t *testing.T) {
	cases := map[string]string{
		"empty":            "",
		"zero":             "0",
		"many zeros":       "000",
		"negative":         "-1",
		"explicit plus":    "+1",
		"fraction":         "1.5",
		"exponent":         "1e3",
		"leading space":    " 1",
		"trailing space":   "1 ",
		"letters":          "abc",
		"mixed":            "12a",
		"arabic-indic":     "١٢٣",
		"fullwidth":        "１２３",
		"hex":              "0x10",
		"underscore":       "1_0",
		"slash":            "/1",
		"one past maximum": "18446744073709551616",
		"far past maximum": "99999999999999999999999",
	}

	for name, raw := range cases {
		if _, err := ParseID(raw); !errors.Is(err, ErrInvalidID) {
			t.Errorf("%s: ParseID(%q) error = %v, want ErrInvalidID", name, raw, err)
		}
	}
}

func TestIDMarshalsAsString(t *testing.T) {
	encoded, err := json.Marshal(struct {
		ID ID `json:"id"`
	}{ID: 18446744073709551615})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	want := `{"id":"18446744073709551615"}`
	if string(encoded) != want {
		t.Errorf("marshal = %s, want %s", encoded, want)
	}
}

func TestIDUnmarshalRoundTrip(t *testing.T) {
	var target struct {
		ID ID `json:"id"`
	}

	if err := json.Unmarshal([]byte(`{"id":"18446744073709551615"}`), &target); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	if target.ID != ID(18446744073709551615) {
		t.Errorf("unmarshalled ID = %d, want 18446744073709551615", target.ID)
	}
}

func TestIDUnmarshalRejectsNonString(t *testing.T) {
	var target struct {
		ID ID `json:"id"`
	}

	if err := json.Unmarshal([]byte(`{"id":7}`), &target); err == nil {
		t.Error("unmarshal of a JSON number succeeded, want error")
	}
}
