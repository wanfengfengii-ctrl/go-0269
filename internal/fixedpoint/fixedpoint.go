// Package fixedpoint provides signed 64-bit fixed-point arithmetic for the
// hatch rate, dead-egg rate, moisture, temperature, relative humidity, and
// spore counts used throughout the silkworm egg cold-storage gate service.
//
// Every operation is integer based: no floating point is used, and length,
// sign, division-by-zero, and overflow are validated before any derived value
// is produced. Comparisons use cross multiplication via arbitrary-precision
// integers so that threshold boundary checks never overflow.
package fixedpoint

import (
	"errors"
	"math"
	"math/big"
	"strconv"
	"strings"
)

// maxScale bounds the number of decimal places to keep arithmetic bounded.
const maxScale = 18

// Value is a signed fixed-point number with an explicit number of decimal
// places (scale). The raw integer N stores the value scaled by 10^scale.
type Value struct {
	N     int64
	Scale int
}

// Sentinel errors returned by fixed-point operations.
var (
	ErrInvalidFormat  = errors.New("fixedpoint: invalid format")
	ErrOverflow       = errors.New("fixedpoint: overflow")
	ErrDivisionByZero = errors.New("fixedpoint: division by zero")
	ErrScaleMismatch  = errors.New("fixedpoint: scale mismatch")
	ErrInvalidScale   = errors.New("fixedpoint: invalid scale")
)

// Parse decodes a decimal string into a fixed-point Value with the given
// scale. Leading sign, optional fraction, length, and digit validity are all
// checked before any magnitude is produced.
func Parse(s string, scale int) (Value, error) {
	if scale < 0 || scale > maxScale {
		return Value{}, ErrInvalidScale
	}
	s = strings.TrimSpace(s)
	if s == "" {
		return Value{}, ErrInvalidFormat
	}
	neg := false
	switch s[0] {
	case '+':
		s = s[1:]
	case '-':
		neg = true
		s = s[1:]
	}
	if s == "" {
		return Value{}, ErrInvalidFormat
	}
	intPart, fracPart := "0", ""
	if i := strings.IndexByte(s, '.'); i >= 0 {
		intPart, fracPart = s[:i], s[i+1:]
	} else {
		intPart = s
	}
	if intPart == "" {
		intPart = "0"
	}
	if !isDigits(intPart) || !isDigits(fracPart) {
		return Value{}, ErrInvalidFormat
	}
	if len(fracPart) > scale {
		return Value{}, ErrInvalidFormat
	}
	for len(fracPart) < scale {
		fracPart += "0"
	}
	n, err := parseDigits(intPart + fracPart)
	if err != nil {
		return Value{}, ErrOverflow
	}
	if neg {
		n = -n
	}
	return Value{N: n, Scale: scale}, nil
}

// String renders the value with its full scale, preserving the decimal point.
func (v Value) String() string {
	neg := v.N < 0
	abs := v.N
	if neg {
		abs = -abs
	}
	s := strconv.FormatInt(abs, 10)
	if v.Scale == 0 {
		if neg {
			return "-" + s
		}
		return s
	}
	for len(s) <= v.Scale {
		s = "0" + s
	}
	out := s[:len(s)-v.Scale] + "." + s[len(s)-v.Scale:]
	if neg {
		out = "-" + out
	}
	return out
}

// Add sums two values of identical scale, checking overflow.
func Add(a, b Value) (Value, error) {
	if a.Scale != b.Scale {
		return Value{}, ErrScaleMismatch
	}
	n, err := addInt64(a.N, b.N)
	if err != nil {
		return Value{}, ErrOverflow
	}
	return Value{N: n, Scale: a.Scale}, nil
}

// Sub subtracts b from a (identical scale), checking overflow.
func Sub(a, b Value) (Value, error) {
	if a.Scale != b.Scale {
		return Value{}, ErrScaleMismatch
	}
	n, err := subInt64(a.N, b.N)
	if err != nil {
		return Value{}, ErrOverflow
	}
	return Value{N: n, Scale: a.Scale}, nil
}

// Mul multiplies two values, producing a value whose scale is the sum of the
// input scales. Overflow is checked.
func Mul(a, b Value) (Value, error) {
	if a.Scale+b.Scale > maxScale {
		return Value{}, ErrInvalidScale
	}
	n, err := mulInt64(a.N, b.N)
	if err != nil {
		return Value{}, ErrOverflow
	}
	return Value{N: n, Scale: a.Scale + b.Scale}, nil
}

// Cmp compares two values using cross multiplication, never using floating
// point and never overflowing.
func Cmp(a, b Value) int {
	if a.Scale == b.Scale {
		return cmpInt64(a.N, b.N)
	}
	an := new(big.Int).Mul(big.NewInt(a.N), pow10Big(b.Scale))
	bn := new(big.Int).Mul(big.NewInt(b.N), pow10Big(a.Scale))
	return an.Cmp(bn)
}

// Rescale converts a value to a new scale, checking overflow on up-scaling
// and truncating on down-scaling.
func Rescale(v Value, scale int) (Value, error) {
	if scale < 0 || scale > maxScale {
		return Value{}, ErrInvalidScale
	}
	if scale == v.Scale {
		return v, nil
	}
	if scale > v.Scale {
		f := pow10(scale - v.Scale)
		n, err := mulInt64(v.N, f)
		if err != nil {
			return Value{}, ErrOverflow
		}
		return Value{N: n, Scale: scale}, nil
	}
	return Value{N: v.N / pow10(v.Scale-scale), Scale: scale}, nil
}

// Rate computes numerator/denominator as a fixed-point ratio at the given
// scale. Division by zero and post-division overflow are rejected.
func Rate(numerator, denominator int64, scale int) (Value, error) {
	if denominator == 0 {
		return Value{}, ErrDivisionByZero
	}
	if scale < 0 || scale > maxScale {
		return Value{}, ErrInvalidScale
	}
	n := new(big.Int).Mul(big.NewInt(numerator), pow10Big(scale))
	n.Div(n, big.NewInt(denominator))
	if !n.IsInt64() {
		return Value{}, ErrOverflow
	}
	return Value{N: n.Int64(), Scale: scale}, nil
}

// Percent computes (numerator/denominator)*100 at the given scale, used for the
// hatch and dead-egg rates expressed as a percentage (e.g. 80.00 means 80%).
// Division by zero, invalid scale, and overflow are all rejected using
// arbitrary-precision integers.
func Percent(numerator, denominator int64, scale int) (Value, error) {
	if denominator == 0 {
		return Value{}, ErrDivisionByZero
	}
	if scale < 0 || scale > maxScale {
		return Value{}, ErrInvalidScale
	}
	n := new(big.Int).Mul(big.NewInt(numerator), big.NewInt(100))
	n.Mul(n, pow10Big(scale))
	n.Div(n, big.NewInt(denominator))
	if !n.IsInt64() {
		return Value{}, ErrOverflow
	}
	return Value{N: n.Int64(), Scale: scale}, nil
}

// Div divides a by b and returns a value at the given scale. Division by zero
// and result overflow are rejected.
func Div(a, b Value, scale int) (Value, error) {
	if b.N == 0 {
		return Value{}, ErrDivisionByZero
	}
	if scale < 0 || scale > maxScale {
		return Value{}, ErrInvalidScale
	}
	an := new(big.Int).Mul(big.NewInt(a.N), pow10Big(b.Scale+scale))
	bn := new(big.Int).Mul(big.NewInt(b.N), pow10Big(a.Scale))
	an.Div(an, bn)
	if !an.IsInt64() {
		return Value{}, ErrOverflow
	}
	return Value{N: an.Int64(), Scale: scale}, nil
}

func isDigits(s string) bool {
	for i := 0; i < len(s); i++ {
		if s[i] < '0' || s[i] > '9' {
			return false
		}
	}
	return true
}

func parseDigits(s string) (int64, error) {
	var n int64
	for i := 0; i < len(s); i++ {
		d := int64(s[i] - '0')
		if n > (math.MaxInt64-d)/10 {
			return 0, ErrOverflow
		}
		n = n*10 + d
	}
	return n, nil
}

func addInt64(a, b int64) (int64, error) {
	if (b > 0 && a > math.MaxInt64-b) || (b < 0 && a < math.MinInt64-b) {
		return 0, ErrOverflow
	}
	return a + b, nil
}

func subInt64(a, b int64) (int64, error) {
	if (b < 0 && a > math.MaxInt64+b) || (b > 0 && a < math.MinInt64+b) {
		return 0, ErrOverflow
	}
	return a - b, nil
}

func mulInt64(a, b int64) (int64, error) {
	if a == 0 || b == 0 {
		return 0, nil
	}
	if a == math.MinInt64 && b == -1 {
		return 0, ErrOverflow
	}
	if b == math.MinInt64 && a == -1 {
		return 0, ErrOverflow
	}
	c := a * b
	if c/b != a {
		return 0, ErrOverflow
	}
	return c, nil
}

func cmpInt64(a, b int64) int {
	switch {
	case a < b:
		return -1
	case a > b:
		return 1
	default:
		return 0
	}
}

func pow10(n int) int64 {
	p := int64(1)
	for i := 0; i < n; i++ {
		p *= 10
	}
	return p
}

func pow10Big(n int) *big.Int {
	return new(big.Int).Exp(big.NewInt(10), big.NewInt(int64(n)), nil)
}
