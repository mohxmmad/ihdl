package internal

type SignalKind string

const (
	SignalBits SignalKind = "bits"
	SignalRGB  SignalKind = "rgb"
	SignalBW   SignalKind = "bw"
)

type Port struct {
	Name  string
	Kind  SignalKind
	Width int
}

type Value struct {
	Kind     SignalKind
	Bits     []bool
	Channels []uint8
}

type Operation struct {
	Kind    string
	Name    string
	Inputs  []string
	Outputs []string
	Module  string
	Signals []string
}

type Circuit struct {
	Name    string
	Path    string
	Inputs  []Port
	Clocks  []Port
	Outputs []Port
	Wires   []Port
	Ops     []Operation
	Imports []string
	Signals map[string]Port
}

type Project struct {
	Entry    *Circuit
	Circuits map[string]*Circuit
}
