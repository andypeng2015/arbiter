package decimal

import (
	"encoding/json"
	"fmt"
	"math/big"
	"strings"
)

// Value is one exact fixed-point decimal with an optional unit symbol.
type Value struct {
	coeff string
	scale int32
	unit  string
}

// Parse parses one decimal string and attaches an optional unit.
func Parse(text, unit string) (Value, error) {
	text = strings.TrimSpace(text)
	if text == "" {
		return Value{}, fmt.Errorf("decimal value is required")
	}

	sign := ""
	switch text[0] {
	case '+':
		text = text[1:]
	case '-':
		sign = "-"
		text = text[1:]
	}
	if text == "" {
		return Value{}, fmt.Errorf("invalid decimal")
	}

	intPart := text
	fracPart := ""
	if dot := strings.IndexByte(text, '.'); dot >= 0 {
		intPart = text[:dot]
		fracPart = text[dot+1:]
	}
	if intPart == "" {
		intPart = "0"
	}
	if !digitsOnly(intPart) || (fracPart != "" && !digitsOnly(fracPart)) {
		return Value{}, fmt.Errorf("invalid decimal %q", text)
	}

	scale := int32(len(fracPart))
	coeff := sign + intPart + fracPart
	return normalize(coeff, scale, unit)
}

// MustParse parses one decimal and panics on error.
func MustParse(text, unit string) Value {
	value, err := Parse(text, unit)
	if err != nil {
		panic(err)
	}
	return value
}

// Unit returns the attached unit symbol.
func (v Value) Unit() string { return v.unit }

// Scale returns the decimal scale.
func (v Value) Scale() int32 { return v.scale }

// Text returns the numeric part without the unit.
func (v Value) Text() string {
	coeff := v.coeff
	if coeff == "" {
		coeff = "0"
	}
	neg := strings.HasPrefix(coeff, "-")
	if neg {
		coeff = coeff[1:]
	}
	if v.scale == 0 {
		if neg && coeff != "0" {
			return "-" + coeff
		}
		return coeff
	}
	for int(v.scale) >= len(coeff) {
		coeff = "0" + coeff
	}
	split := len(coeff) - int(v.scale)
	text := coeff[:split] + "." + coeff[split:]
	if neg && coeff != "0" {
		return "-" + text
	}
	return text
}

func (v Value) String() string {
	text := v.Text()
	if v.unit == "" {
		return text
	}
	return text + " " + v.unit
}

// MarshalJSON encodes decimals as their canonical string form.
func (v Value) MarshalJSON() ([]byte, error) {
	return json.Marshal(v.String())
}

// Equal reports whether two decimals represent the same exact value and unit.
func (v Value) Equal(other Value) bool {
	return v.unit == other.unit && v.coeff == other.coeff && v.scale == other.scale
}

// Cmp compares two decimals with the same unit.
func (v Value) Cmp(other Value) (int, error) {
	if v.unit != other.unit {
		return 0, fmt.Errorf("unit mismatch: %q vs %q", v.unit, other.unit)
	}
	left, right := align(v, other)
	return left.Cmp(right), nil
}

// Add adds two decimals with the same unit.
func (v Value) Add(other Value) (Value, error) {
	if v.unit != other.unit {
		return Value{}, fmt.Errorf("unit mismatch: %q vs %q", v.unit, other.unit)
	}
	left, right := align(v, other)
	sum := new(big.Int).Add(left, right)
	scale := v.scale
	if other.scale > scale {
		scale = other.scale
	}
	return normalize(sum.String(), scale, v.unit)
}

// Sub subtracts two decimals with the same unit.
func (v Value) Sub(other Value) (Value, error) {
	if v.unit != other.unit {
		return Value{}, fmt.Errorf("unit mismatch: %q vs %q", v.unit, other.unit)
	}
	left, right := align(v, other)
	diff := new(big.Int).Sub(left, right)
	scale := v.scale
	if other.scale > scale {
		scale = other.scale
	}
	return normalize(diff.String(), scale, v.unit)
}

// Mul multiplies two decimals. The result unit is the left operand's unit
// (right must be unitless) or both must be unitless.
func (v Value) Mul(other Value) (Value, error) {
	unit := v.unit
	if other.unit != "" {
		if v.unit == "" {
			unit = other.unit
		} else {
			return Value{}, fmt.Errorf("cannot multiply %q by %q — at least one operand must be unitless", v.unit, other.unit)
		}
	}
	a := mustBig(v.coeff)
	b := mustBig(other.coeff)
	product := new(big.Int).Mul(a, b)
	return normalize(product.String(), v.scale+other.scale, unit)
}

// Div divides two decimals. Both must share the same unit (result is unitless)
// or the divisor must be unitless (result keeps the dividend's unit).
func (v Value) Div(other Value, precision int32) (Value, error) {
	if other.coeff == "0" {
		return Value{}, fmt.Errorf("division by zero")
	}
	unit := v.unit
	if other.unit != "" {
		if v.unit == other.unit {
			unit = "" // same-unit division yields unitless ratio
		} else {
			return Value{}, fmt.Errorf("cannot divide %q by %q", v.unit, other.unit)
		}
	}
	if precision <= 0 {
		precision = 10
	}
	// Scale numerator up for precision, then divide.
	a := mustBig(v.coeff)
	b := mustBig(other.coeff)
	pow := new(big.Int).Exp(big.NewInt(10), big.NewInt(int64(precision)), nil)
	scaled := new(big.Int).Mul(a, pow)
	quotient := new(big.Int).Div(scaled, b)
	totalScale := v.scale - other.scale + precision
	if totalScale < 0 {
		// Scale back up if the subtraction went negative.
		pow2 := new(big.Int).Exp(big.NewInt(10), big.NewInt(int64(-totalScale)), nil)
		quotient.Mul(quotient, pow2)
		totalScale = 0
	}
	return normalize(quotient.String(), totalScale, unit)
}

// Mod computes the remainder of dividing two decimals with the same unit.
func (v Value) Mod(other Value) (Value, error) {
	if v.unit != other.unit {
		return Value{}, fmt.Errorf("unit mismatch: %q vs %q", v.unit, other.unit)
	}
	if other.coeff == "0" {
		return Value{}, fmt.Errorf("modulo by zero")
	}
	left, right := align(v, other)
	rem := new(big.Int).Mod(left, right)
	scale := v.scale
	if other.scale > scale {
		scale = other.scale
	}
	return normalize(rem.String(), scale, v.unit)
}

// Abs returns the absolute value.
func (v Value) Abs() Value {
	if strings.HasPrefix(v.coeff, "-") {
		v.coeff = strings.TrimPrefix(v.coeff, "-")
	}
	return v
}

func normalize(coeff string, scale int32, unit string) (Value, error) {
	if coeff == "" || coeff == "+" || coeff == "-" {
		return Value{}, fmt.Errorf("invalid decimal coefficient")
	}
	bi, ok := new(big.Int).SetString(coeff, 10)
	if !ok {
		return Value{}, fmt.Errorf("invalid decimal coefficient %q", coeff)
	}
	ten := big.NewInt(10)
	rem := new(big.Int)
	for scale > 0 {
		q := new(big.Int)
		q.QuoRem(bi, ten, rem)
		if rem.Sign() != 0 {
			break
		}
		bi = q
		scale--
	}
	if bi.Sign() == 0 {
		scale = 0
	}
	return Value{
		coeff: bi.String(),
		scale: scale,
		unit:  unit,
	}, nil
}

func align(left, right Value) (*big.Int, *big.Int) {
	a := mustBig(left.coeff)
	b := mustBig(right.coeff)
	switch {
	case left.scale == right.scale:
		return a, b
	case left.scale < right.scale:
		return scaleBig(a, right.scale-left.scale), b
	default:
		return a, scaleBig(b, left.scale-right.scale)
	}
}

func mustBig(text string) *big.Int {
	bi, ok := new(big.Int).SetString(text, 10)
	if !ok {
		panic("invalid decimal coefficient")
	}
	return bi
}

func scaleBig(value *big.Int, scale int32) *big.Int {
	if scale <= 0 {
		return new(big.Int).Set(value)
	}
	pow := new(big.Int).Exp(big.NewInt(10), big.NewInt(int64(scale)), nil)
	return new(big.Int).Mul(value, pow)
}

func digitsOnly(text string) bool {
	if text == "" {
		return false
	}
	for _, r := range text {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}
