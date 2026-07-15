# iHDL

`iHDL` is a small HDL-like language for describing and simulating logic modules in Go.

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
- rising-edge flip-flops: `DFF`, `TFF`, `SRFF`, `JKFF`
- interactive simulation
- file-based simulation with `.iinp` and `.iout`
- typed pixels: RGB and grayscale/BW
- display windows with `.ippf` export

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

## 4. Display

Displays render pixel signals into a separate viewer window during interactive simulation.

```ihdl
DISPLAY SCREEN 2 1
PIXEL SCREEN 0 0 PX
PIXEL SCREEN 1 0 LEVEL
```

Rules:

- `DISPLAY Name Width Height` declares a screen
- `PIXEL Display X Y Signal` maps one `RGB` or `BW` signal to one pixel
- `BW` pixels are shown as grayscale
- if a mapped signal has no value, that pixel stays black
- unmapped pixels also stay black
- display names must be declared before their `PIXEL` mappings

## 5. Clock

Clock is declared separately:

```ihdl
CLOCK CLK
```

Rules:

- clock must be 1 bit
- clock is read as a source signal during simulation
- sequential elements trigger on the rising edge (`0 -> 1`)
- flip-flops start at `0` unless set by an earlier clock edge in the same simulation session

## 6. Gates

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

## 7. Constants

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

## 8. Flip-Flops

All flip-flops are rising-edge triggered and keep their state across repeated evaluations in the same simulator session.

### DFF

```ihdl
DFF FF1 D CLK Q
```

- `D` and `Q` can be 1-bit signals or bit buses
- `CLK` must be 1 bit

### TFF

```ihdl
TFF FF1 T CLK Q
```

- `T`, `CLK`, and `Q` must be 1 bit
- on a rising edge, `Q` toggles only when `T = 1`

### SRFF

```ihdl
SRFF FF1 S R CLK Q
```

- `S`, `R`, `CLK`, and `Q` must be 1 bit
- on a rising edge: `10` sets, `01` resets, `00` holds
- `11` is treated as an invalid state

### JKFF

```ihdl
JKFF FF1 J K CLK Q
```

- `J`, `K`, `CLK`, and `Q` must be 1 bit
- on a rising edge: `10` sets, `01` resets, `00` holds, `11` toggles

## 9. Splitter

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

## 10. Join

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

## 11. Modules

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

## 12. Internal Wires

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

## 13. Running the Simulator

### Build the binary

Linux:

```bash
go build -o ihdl ./cmd/ihdl
./ihdl examples/and.ihdl
```

Windows PowerShell:

```powershell
go build -o ihdl.exe .\cmd\ihdl
.\ihdl.exe .\examples\and.ihdl
```

### Interactive mode

```bash
go run ./cmd/ihdl examples/and.ihdl
```

The simulator asks for values based on the module's declared inputs and clocks.

If the module declares a `DISPLAY`, iHDL also starts a local viewer window in your browser and refreshes the rendered frame as you simulate new inputs.

Examples:

- bit: `1`
- bus `[4]`: `1010`
- RGB: `255,128,0`
- RGB binary: `11111111,10000000,00000000`
- BW decimal: `200`
- BW binary byte: `11001000`

### File mode

```bash
go run ./cmd/ihdl examples/and.ihdl -iinp examples/and.iinp -iout examples/and.iout
```

Linux binary:

```bash
./ihdl examples/and.ihdl -iinp examples/and.iinp -iout examples/and.iout
```

Windows PowerShell binary:

```powershell
.\ihdl.exe .\examples\and.ihdl -iinp .\examples\and.iinp -iout .\examples\and.iout
```

Both flags must be given together.

To export the latest rendered display frame, use:

```bash
go run ./cmd/ihdl examples/display_rgb.ihdl -iinp frame.iinp -iout frame.iout -ippf screen.ippf
```

Linux binary:

```bash
./ihdl examples/display_rgb.ihdl -iinp frame.iinp -iout frame.iout -ippf screen.ippf
```

Windows PowerShell binary:

```powershell
.\ihdl.exe .\examples\display_rgb.ihdl -iinp .\frame.iinp -iout .\frame.iout -ippf .\screen.ippf
```

`.ippf` files are written with PNG image data using the custom `.ippf` extension.

### Release binaries

GitHub releases include prebuilt binaries for:

- Linux: `ihdl-linux-amd64`
- Windows: `ihdl-windows-amd64.exe`

Run them the same way as the examples above, replacing the local build name with the downloaded binary name.

## 14. `.iinp` Format

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

## 15. `.iout` Format

One output per line:

```text
OUT 1
SUM 10101010
PX_OUT 255,64,0
BW_OUT 170
```

## 16. Example Modules

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

### Display with RGB and BW pixels

```ihdl
MODULE DisplayRGB

INPUT_RGB P0
INPUT_BW P1
OUTPUT_BW LEVEL

DISPLAY SCREEN 2 1
PIXEL SCREEN 0 0 P0
PIXEL SCREEN 1 0 P1

BUF B1 P1 LEVEL
```

### D flip-flop

```ihdl
MODULE DFFExample

INPUT D
CLOCK CLK
OUTPUT Q

DFF FF1 D CLK Q
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

## 17. Current Limitations

- no direct bit indexing like `DATA[0]`
- no pixel arithmetic or pixel logic operations yet
- `AND`, `OR`, `NOT`, `HIGH`, `LOW`, `SPLIT` are bit-only
- pixels are currently routed with `BUF` and modules
