# The icon

A warm graphite squircle for a terminal that stays on. The W is drawn in one
stroke; its last stroke is alive in jade. It reads as the brand and as a caret,
an agent still typing.

- `icon.svg` is the app icon. `icon-dev.svg` is the development app's, its live
  stroke in gold so a dev build is not mistaken for the released app.
- `../resources/icon.png` and `../resources/icon-dev.png` are the 1024px renders
  MyGo finds and packages. Render the SVG at 2048 and downsample to 1024 so the
  small `.icns` entries stay clean.

Palette: surface `#2d2b26` → `#171714` → `#0a0a0b`; letter `#fffdf6` →
`#f1ece0` → `#c9c2b2`; live stroke `#c9f4d8` → `#7bd3a1` → `#3d956a`; dev live
stroke `#ffe8bf` → `#f0b45c` → `#b3702a`.
