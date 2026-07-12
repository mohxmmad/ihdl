# iHDL

`iHDL` is a small HDL-like language for describing and simulating combinational logic modules in Go.

Current features:

- logic gates: `AND`, `OR`, `NOT`
- utility copy op: `BUF`
- constants: `HIGH`, `LOW`
- buses: `NAME[n]`
- bit splitters: `SPLIT`
- bit joins: `JOIN`
- reusable modules
- module imports by module name
- explicit module instantiation
- internal wires
- clocks as source ports
- interactive simulation
- file-based simulation with `.iinp` and `.iout`
- typed pixels: RGB and grayscale/BW

## 1. File Structure

An iHDL file starts with a module declaration:

```ihdl
MODULE AndGate
```

You can then declare ports, wires, and operations.

## 2. Bit Signals

Single-bit signals:

```ihdl
INPUT A
OUTPUT OUT
WIRE TEMP
```

Bus signals:

```ihdl
INPUT DATA[8]
OUTPUT RESULT[8]
WIRE TMP[4]
```

## 3. Pixel Signals

RGB pixels:

```ihdl
INPUT_RGB PX
OUTPUT_RGB PX_OUT
WIRE_RGB MID
```

BW pixels:

```ihdl
INPUT_BW LEVEL
OUTPUT_BW LEVEL_OUT
WIRE_BW TMP_BW
```

Rules:

- `RGB` values have 3 channels: `r,g,b`
- each RGB channel is `0..255`
- each RGB channel can also be provided as 8-bit binary
- `BW` values are `0..255`
- `BW` input can also be provided as 8-bit binary like `10101010`

## 4. Clock

Clock is declared separately:

```ihdl
CLOCK CLK
```

Rules:

- clock must be 1 bit
- clock behaves like an input source right now
- clock is reserved for future sequential logic

## 5. Gates

### AND

```ihdl
AND G1 A B OUT
```

### OR

```ihdl
OR G1 A B OUT
```

### NOT

```ihdl
NOT G1 IN OUT
```

### BUF

`BUF` copies a signal unchanged.

```ihdl
BUF B1 IN OUT
```

`BUF` works for:

- bit signals
- buses
- RGB pixels
- BW pixels

Notes:

- `AND`, `OR`, `NOT` only work on bit signals and buses
- both `AND` inputs must have the same width
- both `OR` inputs must have the same width
- `NOT` preserves width

## 6. Constants

```ihdl
HIGH VCC
LOW GND
```

For buses:

```ihdl
WIRE ONES[8]
HIGH ONES
```

Rules:

- `HIGH` fills every bit with `1`
- `LOW` fills every bit with `0`
- constants only work on bit signals and buses

## 7. Splitter

Use `SPLIT` to split a bit bus into 1-bit outputs.

```ihdl
INPUT DATA[3]
OUTPUT B0
OUTPUT B1
OUTPUT B2

SPLIT DATA B0 B1 B2
```

Rules:

- splitter only works on bit buses
- number of outputs must match bus width
- each split output is 1 bit

## 8. Join

Use `JOIN` to combine 1-bit signals into a bus.

```ihdl
INPUT B0
INPUT B1
INPUT B2
OUTPUT BUS[3]

JOIN B0 B1 B2 BUS
```

Rules:

- join only works on bit signals
- each join input must be 1 bit
- output width must match the number of joined inputs
- input order becomes bus bit order

## 9. Modules

### Define a module

```ihdl
MODULE NotGate

INPUT IN
OUTPUT OUT

NOT N1 IN OUT
```

### Import a module

```ihdl
IMPORT NotGate
```

Imports use the module name, not the filename.

### Instantiate a module

```ihdl
NotGate U1 IN OUT
```

Format:

```ihdl
ModuleName InstanceName signal1 signal2 signal3 ...
```

Signal order is:

1. all child `INPUT`s in declaration order
2. all child `CLOCK`s in declaration order
3. all child `OUTPUT`s in declaration order

Example with clock:

```ihdl
MODULE Child
INPUT A
CLOCK CLK
OUTPUT OUT
BUF B1 A OUT
```

Instantiate it like:

```ihdl
Child U1 A CLK OUT
```

## 10. Internal Wires

Use `WIRE`, `WIRE_RGB`, or `WIRE_BW` for temporary values.

Example:

```ihdl
MODULE Top

IMPORT AndGate

INPUT A
INPUT B
INPUT C
OUTPUT OUT
WIRE TEMP1

AndGate A1 A B TEMP1
AndGate A2 TEMP1 C OUT
```

## 11. Running the Simulator

### Interactive mode

```bash
go run ./cmd/ihdl -i examples/and.ihdl
```

The simulator asks for values based on the module's declared inputs and clocks.

Examples:

- bit: `1`
- bus `[4]`: `1010`
- RGB: `255,128,0`
- RGB binary: `11111111,10000000,00000000`
- BW decimal: `200`
- BW binary byte: `11001000`

### File mode

```bash
go run ./cmd/ihdl -i examples/and.ihdl -iinp examples/and.iinp -iout examples/and.iout
```

Both flags must be given together.

## 12. `.iinp` Format

One source signal per line:

```text
A 1
B 0
CLK 1
DATA 10101010
PX 255,64,0
PX 11111111,01000000,00000000
LEVEL 10101010
```

Rules:

- bit bus values are binary strings
- RGB values are `r,g,b`
- each RGB channel may be decimal `0..255` or 8-bit binary
- BW values can be decimal `0..255`
- BW values can also be 8-bit binary

## 13. `.iout` Format

One output per line:

```text
OUT 1
SUM 10101010
PX_OUT 255,64,0
BW_OUT 170
```

## 14. Example Modules

### AND gate

```ihdl
MODULE AndGate

INPUT A
INPUT B
OUTPUT OUT

AND G1 A B OUT
```

### Bus inverter

```ihdl
MODULE BusNot

INPUT DATA[3]
OUTPUT FINAL[3]

NOT N1 DATA FINAL
```

### Join bits into a bus

```ihdl
MODULE JoinExample

INPUT B0
INPUT B1
INPUT B2
OUTPUT BUS[3]

JOIN B0 B1 B2 BUS
```

### RGB passthrough

```ihdl
MODULE RGBPassthrough

INPUT_RGB PX
INPUT_BW LEVEL
OUTPUT_RGB PX_OUT
OUTPUT_BW BW_OUT

BUF B1 PX PX_OUT
BUF B2 LEVEL BW_OUT
```

### 3-pixel row module

```ihdl
MODULE RGBRow

IMPORT RGBPassthrough

INPUT_RGB P1
INPUT_RGB P2
INPUT_RGB P3
INPUT_BW L1
INPUT_BW L2
INPUT_BW L3
OUTPUT_RGB O1
OUTPUT_RGB O2
OUTPUT_RGB O3
OUTPUT_BW B1
OUTPUT_BW B2
OUTPUT_BW B3

RGBPassthrough C1 P1 L1 O1 B1
RGBPassthrough C2 P2 L2 O2 B2
RGBPassthrough C3 P3 L3 O3 B3
```

You can build larger structures like `3x3` grids by composing row or cell modules the same way.

## 15. Current Limitations

- no sequential elements yet, only clock declaration support
- no direct bit indexing like `DATA[0]`
- no pixel arithmetic or pixel logic operations yet
- `AND`, `OR`, `NOT`, `HIGH`, `LOW`, `SPLIT` are bit-only
- pixels are currently routed with `BUF` and modules

## 16. Recommended Next Steps

Natural next features are:

1. direct bit indexing
2. registers / flip-flops using `CLOCK`
3. pixel operations like mix, threshold, invert, grayscale conversion
4. exporting pixel outputs to an image format like `.ppm`
