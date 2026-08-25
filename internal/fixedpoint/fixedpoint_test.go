package fixedpoint

import (
	"errors"
	"testing"
)

func TestParse(t *testing.T) {
	tests := []struct {
		in    string
		scale int
		want  Value
	}{
		{"12.34", 2, Value{N: 1234, Scale: 2}},
		{"0.5", 2, Value{N: 50, Scale: 2}},
		{"-3.25", 2, Value{N: -325, Scale: 2}},
		{"+7", 2, Value{N: 700, Scale: 2}},
		{"100", 0, Value{N: 100, Scale: 0}},
		{"0.000", 3, Value{N: 0, Scale: 3}},
	}
	for _, tt := range tests {
		got, err := Parse(tt.in, tt.scale)
		if err != nil {
			t.Fatalf("Parse(%q,%d) error: %v", tt.in, tt.scale, err)
		}
		if got != tt.want {
			t.Fatalf("Parse(%q,%d) = %+v, want %+v", tt.in, tt.scale, got, tt.want)
		}
	}
}

func TestParseInvalid(t *testing.T) {
	tests := []struct {
		in    string
		scale int
	}{
		{"", 2},
		{"abc", 2},
		{"1.2.3", 2},
		{"1.2345", 2}, // too many fractional digits
		{"--1", 2},
		{"+", 2},
		{"1", -1},
		{"1", 19},
	}
	for _, tt := range tests {
		if _, err := Parse(tt.in, tt.scale); !errors.Is(err, ErrInvalidFormat) && !errors.Is(err, ErrInvalidScale) {
			t.Fatalf("Parse(%q,%d) error = %v, want format/scale error", tt.in, tt.scale, err)
		}
	}
}

func TestAddSubMul(t *testing.T) {
	a := Value{N: 1234, Scale: 2}
	b := Value{N: 100, Scale: 2}
	sum, err := Add(a, b)
	if err != nil || sum != (Value{N: 1334, Scale: 2}) {
		t.Fatalf("Add = %+v, %v", sum, err)
	}
	diff, err := Sub(a, b)
	if err != nil || diff != (Value{N: 1134, Scale: 2}) {
		t.Fatalf("Sub = %+v, %v", diff, err)
	}
	prod, err := Mul(Value{N: 3, Scale: 0}, Value{N: 4, Scale: 0})
	if err != nil || prod != (Value{N: 12, Scale: 0}) {
		t.Fatalf("Mul = %+v, %v", prod, err)
	}
	// Scale mismatch must be rejected.
	if _, err := Add(a, Value{N: 5, Scale: 1}); !errors.Is(err, ErrScaleMismatch) {
		t.Fatalf("Add mismatched scale error = %v, want ErrScaleMismatch", err)
	}
}

func TestCmpCrossMultiplication(t *testing.T) {
	a := Value{N: 1, Scale: 0}   // 1
	b := Value{N: 150, Scale: 2} // 1.50
	if Cmp(a, b) != -1 {
		t.Fatalf("Cmp(1, 1.5) = %d, want -1", Cmp(a, b))
	}
	if Cmp(b, a) != 1 {
		t.Fatalf("Cmp(1.5, 1) = %d, want 1", Cmp(b, a))
	}
	c := Value{N: 100, Scale: 2} // 1.00
	if Cmp(a, c) != 0 {
		t.Fatalf("Cmp(1, 1.00) = %d, want 0", Cmp(a, c))
	}
}

func TestRateAndDiv(t *testing.T) {
	r, err := Rate(3, 4, 2)
	if err != nil || r != (Value{N: 75, Scale: 2}) {
		t.Fatalf("Rate = %+v, %v", r, err)
	}
	if _, err := Rate(1, 0, 2); !errors.Is(err, ErrDivisionByZero) {
		t.Fatalf("Rate by zero error = %v, want ErrDivisionByZero", err)
	}
	d, err := Div(Value{N: 1, Scale: 0}, Value{N: 2, Scale: 0}, 2)
	if err != nil || d != (Value{N: 50, Scale: 2}) {
		t.Fatalf("Div = %+v, %v", d, err)
	}
}

func TestOverflow(t *testing.T) {
	big := Value{N: 9223372036854775807, Scale: 0}
	if _, err := Add(big, Value{N: 1, Scale: 0}); !errors.Is(err, ErrOverflow) {
		t.Fatalf("Add overflow error = %v, want ErrOverflow", err)
	}
	if _, err := Mul(big, Value{N: 2, Scale: 0}); !errors.Is(err, ErrOverflow) {
		t.Fatalf("Mul overflow error = %v, want ErrOverflow", err)
	}
	if _, err := Parse("99999999999999999999999999", 0); !errors.Is(err, ErrOverflow) {
		t.Fatalf("Parse overflow error = %v, want ErrOverflow", err)
	}
}

func TestString(t *testing.T) {
	if got := (Value{N: -325, Scale: 2}).String(); got != "-3.25" {
		t.Fatalf("String = %q, want -3.25", got)
	}
	if got := (Value{N: 700, Scale: 2}).String(); got != "7.00" {
		t.Fatalf("String = %q, want 7.00", got)
	}
}
