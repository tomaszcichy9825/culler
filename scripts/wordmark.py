"""Generate the culler wordmark lockups with the text converted to outlines.

Emits assets/brand/wordmark.svg (hero, on dark) and wordmark-light.svg
(the DESIGN-SPEC 5.2 "on light" row), so neither file needs JetBrains Mono
installed to render correctly.

Only needed when the lockups themselves change; the generated SVGs are
committed. Requires fontTools and JetBrains Mono:

    brew install --cask font-jetbrains-mono
    pip install fonttools
"""
import os
from fontTools.ttLib import TTFont
from fontTools.pens.svgPathPen import SVGPathPen

FONT = os.path.expanduser("~/Library/Fonts/JetBrainsMono-Bold.ttf")
OUT = os.path.join(os.path.dirname(os.path.dirname(os.path.abspath(__file__))), "assets", "brand")
WORD = "culler"


def lockup(path, mark_svg, mark_size, gap, size, colour, tracking_em):
    font = TTFont(FONT)
    upm = font["head"].unitsPerEm
    hhea = font["hhea"]
    asc, desc = hhea.ascender / upm, hhea.descender / upm
    cmap = font.getBestCmap()
    gs = font.getGlyphSet()
    hmtx = font["hmtx"]

    scale = size / upm
    tracking = tracking_em * size

    # CSS emulation: a line-height:1 box of `size`, centred against the mark.
    # Half-leading is negative here because JetBrains Mono's hhea content area
    # (asc - desc) is taller than 1em.
    box_top = (mark_size - size) / 2
    half_leading = (size - (asc - desc) * size) / 2
    baseline = box_top + half_leading + asc * size

    pieces, pen_x = [], 0.0
    for ch in WORD:
        g = cmap[ord(ch)]
        pen = SVGPathPen(gs, ntos=lambda v: f"{v:.2f}")
        gs[g].draw(pen)
        d = pen.getCommands()
        if d:
            # Glyph units are y-up; flip and scale into user space.
            pieces.append(
                f'    <path transform="translate({pen_x + mark_size + gap:.2f} '
                f'{baseline:.2f}) scale({scale:.6f} {-scale:.6f})" d="{d}"/>'
            )
        pen_x += hmtx[g][0] * scale + tracking

    # Trailing tracking is not ink, so it is not part of the lockup's width.
    width = mark_size + gap + pen_x - tracking
    body = "\n".join(pieces)
    with open(os.path.join(OUT, path), "w") as fh:
        fh.write(
            f"""<!-- {'Hero' if size == 56 else 'On light'} horizontal lockup: {mark_size}px mark, {gap}px gap,
     "culler" in JetBrains Mono 700 at {size}px, tracking {tracking_em:+g}em, {colour}.
     Text is outlined, so the font is not needed to render this file.
     Regenerate with scripts/wordmark.py if the lockup changes. -->
<svg viewBox="0 0 {width:.2f} {mark_size}" width="{width:.2f}" height="{mark_size}"
     xmlns="http://www.w3.org/2000/svg">
{mark_svg}
  <g fill="{colour}">
{body}
  </g>
</svg>
"""
        )
    print(path, f"width={width:.2f}", f"baseline={baseline:.2f}")


lockup(
    "wordmark.svg",
    """  <rect x="25.5" y="1.5" width="49" height="49" rx="5"
        fill="none" stroke="#3a4550" stroke-width="3"/>
  <rect x="0"    y="24"  width="52" height="52" rx="5" fill="#56b6c2"/>""",
    mark_size=76, gap=22, size=56, colour="#eef1f6", tracking_em=-0.02,
)

lockup(
    "wordmark-light.svg",
    """  <rect x="13.25" y="1.25" width="25.5" height="25.5" rx="3"
        fill="none" stroke="#b6bdc9" stroke-width="2.5"/>
  <rect x="0"     y="12"   width="28"   height="28"   rx="3" fill="#0e8792"/>""",
    mark_size=40, gap=14, size=30, colour="#12151b", tracking_em=-0.02,
)
