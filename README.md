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

Clocks behave as source signals (like `INPUT`) but can be toggled during interactive simulation to drive sequential logic.

Rules:

- clock must be 1 bit
- clock is prompted at startup alongside `INPUT` ports (can be set to `0`, `1`, or skipped as `err`)
- clocks appear in module signal order for `USE` (after `INPUT`s, before `OUTPUT`s)

### Clock modes

Three modes control a clock during interactive simulation:

- **Manual** (default): the clock holds its initial value until you toggle it
- **Auto**: the clock toggles automatically at a given frequency
- **Step**: advance one half or full cycle manually

### Step a clock

```text
clock step CLK half    # toggle once (0→1 or 1→0)
clock step CLK full    # toggle twice (back to original value)
```

`half` advances one half-cycle (toggles the bit). `full` completes a full cycle (toggles twice, returning to the original value). After stepping, all outputs are recomputed.

### Auto clock

```text
clock auto CLK 0.5     # toggle every 0.5 seconds
```

The clock toggles automatically at the given frequency (in Hz). Each half-cycle toggle triggers a full recomputation of all outputs. Auto-ticking continues until you stop it.

### Stop auto clock

```text
clock manual CLK       # stop auto ticking, return to manual control
```

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
- `AND` and `OR` use **short-circuit evaluation**: if one input determines the result regardless of the other, the gate resolves even when the other input is unknown or an error (e.g. `AND(X, 0)` → `0`, `OR(X, 1)` → `1`)

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

## 8. Splitter

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

## 9. Join

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

## 10. Modules

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

Short-circuit evaluation works through module boundaries: if a sub-module input is unknown or missing but the output is determined by other inputs (e.g. `AND(err, 0)` inside the child), the module resolves correctly instead of propagating an error.

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

## 11. Internal Wires

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

### Wire state persistence

Wires remember their last computed value across evaluations. This enables feedback loops where a wire's value from the previous evaluation is available if the operations driving it haven't resolved yet.

Example feedback loop:

```ihdl
WIRE FB
AND G1 A FB OUT
BUF B1 OUT FB
```

On the first evaluation, `FB` has no value so `AND G1` defers. After `OUT` is resolved, `FB` gets the value of `OUT` and is saved. On the next evaluation, `FB` starts with its saved value, allowing `AND G1` to potentially resolve immediately.

Rules:

- only non-`err` wire values are saved
- input and clock values are never remembered across evaluations — they're always provided by the user or caller
- wire state is scoped per module instance, so different instantiations of the same module have independent wire state

## 12. Evaluation Model

### Multi-pass resolution

The simulator evaluates all operations in repeated passes until no progress is made:

1. Each pass tries every pending operation
2. Operations whose inputs are all ready produce outputs and are consumed
3. Operations with missing inputs stay pending for the next pass
4. When a pass makes no progress, all remaining pending outputs become `err`

This allows chains of dependent operations to resolve across multiple passes without requiring a specific declaration order.

### Short-circuit evaluation

`AND` and `OR` gates attempt short-circuit evaluation: if one input is available (even as `err`) and determines the result, the gate resolves immediately:

- `AND(err, 0)` → `0`
- `AND(err, 1)` → `err` (uncertain, stays pending → becomes `err`)
- `OR(err, 1)` → `1`
- `OR(err, 0)` → `err`

Short-circuit evaluation works through module boundaries: when a sub-module input is `err` or missing (no operation produces it), the simulator passes `err` as the child input and lets the child's own short-circuit evaluation handle it.

### Unresolved signals

Any signal that cannot be resolved after all passes is displayed as `err`:

- skipped input ports
- wires declared but never assigned
- operations whose other inputs are also `err`
- feedback loops that never stabilize

Wire state persistence (Section 11) can help certain feedback patterns stabilize across evaluations. Running `show` or triggering a clock step re-evaluates all operations, which may resolve signals that were `err` in a previous run.

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

The simulator asks for initial values once, then keeps running until you enter `stop`.

Skipping a signal (pressing Enter without a value) marks it as unknown/`err`. The simulator will still try to resolve outputs through short-circuit evaluation where possible.

After startup you can use:

- `set <signal> <value>` to change one source signal and immediately recompute outputs
- `<signal> <value>` as a shorter form of `set`
- `clock auto <clock> <hz>` to start automatic half-cycle toggling for a clock source
- `clock manual <clock>` to stop automatic ticking and return that clock to manual control
- `clock step <clock> half` to advance one half cycle manually
- `clock step <clock> full` to advance two half cycles manually
- `show` to recompute and print outputs again without changing inputs
- `stop` to end the simulation session

Output values appear as:
- `0` / `1` for bit signals
- binary strings for bit buses (e.g. `1010`)
- `r,g,b` tuples for RGB pixels
- decimal or binary for BW pixels
- `err` for unresolved or unknown signals

If the module declares a `DISPLAY`, iHDL also starts a local viewer window in your browser and refreshes the rendered frame after each change.

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
- no sequential/stateful elements (no flip-flops, no latches)
