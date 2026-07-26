package internal

import "fmt"

type SignalKind string

const (
	SignalBits SignalKind = "bits"
	SignalRGB  SignalKind = "rgb"
	SignalBW   SignalKind = "bw"
	SignalGrid SignalKind = "grid"
	SignalErr  SignalKind = "err"
)

type Port struct {
	Name  string
	Kind  SignalKind
	Width int
	GridW int
	GridH int
}

type Value struct {
	Kind     SignalKind
	Bits     []bool
	Channels []uint8
	GridW    int
	GridH    int
	Pixels   []uint8
}

func cloneValue(value Value) Value {
	return Value{Kind: value.Kind, Bits: append([]bool(nil), value.Bits...), Channels: append([]uint8(nil), value.Channels...), GridW: value.GridW, GridH: value.GridH, Pixels: append([]uint8(nil), value.Pixels...)}
}

func errValue() Value {
	return Value{Kind: SignalErr}
}

func bitsToChannel(bits []bool) (uint8, error) {
	if len(bits) != 8 {
		return 0, fmt.Errorf("need exactly 8 bits for channel conversion, got %d", len(bits))
	}
	var v uint8
	for _, b := range bits {
		v <<= 1
		if b {
			v |= 1
		}
	}
	return v, nil
}

func channelToBits(v uint8) []bool {
	bits := make([]bool, 8)
	for i := 7; i >= 0; i-- {
		bits[i] = v&1 == 1
		v >>= 1
	}
	return bits
}

func bitsToRGB(bits []bool) ([3]uint8, error) {
	if len(bits) != 24 {
		return [3]uint8{}, fmt.Errorf("need exactly 24 bits for RGB conversion, got %d", len(bits))
	}
	var ch [3]uint8
	for c := 0; c < 3; c++ {
		var v uint8
		for i := 0; i < 8; i++ {
			v <<= 1
			if bits[c*8+i] {
				v |= 1
			}
		}
		ch[c] = v
	}
	return ch, nil
}

func rgbToBits(channels [3]uint8) []bool {
	bits := make([]bool, 24)
	for c := 0; c < 3; c++ {
		v := channels[c]
		for i := 7; i >= 0; i-- {
			bits[c*8+i] = v&1 == 1
			v >>= 1
		}
	}
	return bits
}

func ConvertValue(value Value, targetKind SignalKind) (Value, error) {
	if value.Kind == targetKind {
		return cloneValue(value), nil
	}
	if value.Kind == SignalErr || targetKind == SignalErr {
		return errValue(), nil
	}
	switch value.Kind {
	case SignalBits:
		switch targetKind {
		case SignalBW:
			v, err := bitsToChannel(value.Bits)
			if err != nil {
				return Value{}, err
			}
			return Value{Kind: SignalBW, Channels: []uint8{v}}, nil
		case SignalRGB:
			ch, err := bitsToRGB(value.Bits)
			if err != nil {
				return Value{}, err
			}
			return Value{Kind: SignalRGB, Channels: []uint8{ch[0], ch[1], ch[2]}}, nil
		}
	case SignalBW:
		if targetKind == SignalBits {
			return Value{Kind: SignalBits, Bits: channelToBits(value.Channels[0])}, nil
		}
	case SignalRGB:
		if targetKind == SignalBits {
			return Value{Kind: SignalBits, Bits: rgbToBits([3]uint8{value.Channels[0], value.Channels[1], value.Channels[2]})}, nil
		}
	case SignalGrid:
		if targetKind == SignalGrid {
			return cloneValue(value), nil
		}
	}
	return Value{}, fmt.Errorf("cannot convert %s to %s", value.Kind, targetKind)
}

func CompatibleKinds(a, b SignalKind) bool {
	if a == b {
		return true
	}
	if a == SignalBits && (b == SignalBW || b == SignalRGB) {
		return true
	}
	if b == SignalBits && (a == SignalBW || a == SignalRGB) {
		return true
	}
	return false
}

type DisplayPixel struct {
	X      int
	Y      int
	Signal string
}

type Display struct {
	Name   string
	Width  int
	Height int
	Pixels []DisplayPixel
	Grids  []DisplayGrid
}

type DisplayGrid struct {
	GridName string
	X        int
	Y        int
}

type Button struct {
	Name  string
	Key   string
	Value []bool
}

type DisplayFrame struct {
	Name   string
	Scope  string
	Width  int
	Height int
	Pixels []uint8
}

type Operation struct {
	Kind    string
	Name    string
	Inputs  []string
	Outputs []string
	Module  string
	Signals []string
	X       int
	Y       int
}

type Circuit struct {
	Name     string
	Path     string
	Inputs   []Port
	Clocks   []Port
	Outputs  []Port
	Wires    []Port
	Buttons  []Button
	Displays []Display
	Ops      []Operation
	Imports  []string
	Signals  map[string]Port
}

type Project struct {
	Entry       *Circuit
	Circuits    map[string]*Circuit
	Frames      map[string]DisplayFrame
	WireState   map[string]map[string]Value
	ButtonState map[string]Value
}
