# RUSH icon v2

## Research and practical decisions

Reviewed v1 first: the amber/yellow/red identity works, but photographic reflections, cap grooves and surface texture dominate. The upright, front-on bottle feels like a catalogue shot; the H runs off the label. Keep the bottle, simplify its materials, give it a deliberate diagonal and real foreshortening, and inset all lettering.

- **One strong idea.** Apple's [App icons HIG](https://developer.apple.com/design/human-interface-guidelines/app-icons) favors a distinctive core concept expressed with few shapes. For RUSH, the cap–neck–shoulder–body silhouette and yellow label must identify the bottle before anyone reads it. Small print is a large-size reward, never the recognition mechanism.
- **Canvas and grid.** Apple's [Icon Composer documentation](https://developer.apple.com/documentation/xcode/creating-your-app-icon-using-icon-composer) specifies a 1024×1024 Mac canvas and recommends its [current templates](https://developer.apple.com/design/resources/). Tahoe uses a revised, rounder rounded-rectangle mask and balanced internal spacing. Do not confuse a finished Dock PNG's transparent outer padding with Composer's source canvas. For these requested standalone PNGs, target a centered tile roughly 824–832 px across with about 96–100 px external padding; that is our optical export target, not a claimed universal HIG safe-area measurement. Keep the entire bottle and shadow within the tile.
- **macOS 26 / Liquid Glass.** Apple's [WWDC25 icon design session](https://developer.apple.com/videos/play/wwdc2025/220/) favors simpler, thicker shapes, soft background gradients and deliberate layering. It warns that elaborate realistic perspective competes with glass effects, but explicitly retains useful perspective in Preview's lens. Our compromise: obvious bottle lean and moderate depth, broad highlights, no photographic microtexture or neon scenery.
- **Layering versus a flattened PNG.** [Create icons with Icon Composer](https://developer.apple.com/videos/play/wwdc2025/361/) separates artwork by depth/color, imports SVG or transparent PNG layers, and supplies dynamic material effects. Composer source layers should omit the final mask and baked-in effects. These deliverables are finished raster concepts with a gentle rendered shadow; they are not editable `.icon` files. A production Composer adaptation would separate background, bottle/cap, label and lettering, then tune materials and appearance variants there.

## Reference icons

These are visual design observations, not claims that every app follows identical rules. Inspected the installed Apple, Arc and Tower icon resources directly (Xcode and Instruments are beta editions), alongside web references. Historical object-rich Mac icons and Tahoe's simpler versions are different generations.

| Reference | What to borrow for RUSH |
|---|---|
| [Xcode](https://developer.apple.com/xcode/) | The diagonal hammer makes one confident gesture; broad head/handle planes imply depth without product-photo texture. Keep the bottle similarly decisive. |
| [Instruments](https://developer.apple.com/xcode/) | The installed icon is a luminous dial, not an elaborate instrument still life. Large needle, dark field, restrained highlights: prioritize the dominant shape over fine ticks. |
| [Preview](https://developer.apple.com/videos/play/wwdc2025/220/) | A large lens overlaps a quiet image tile. The ellipse, rim and overlap communicate depth; perspective has a purpose. Use the cap ellipse and shoulder overlap the same way. |
| [Script Editor](https://support.apple.com/guide/script-editor/welcome/mac) | Diagonal curled paper uses soft shading and clean overlapping planes. Rotation and material cues can coexist with very little surface detail. |
| [Terminal](https://support.apple.com/guide/terminal/welcome/mac) | Almost frontal; a high-contrast prompt does the work. The subtle dark gradient and edge treatment stay secondary. |
| [Keychain Access](https://support.apple.com/guide/keychain-access/welcome/mac) | Fanned keys and overlap produce depth and motion; broad metal highlights stay legible. Avoid reproducing every physical detail. |
| [Things](https://culturedcode.com/things/) | A bold check and blue/white contrast dominate; shallow depth supports the symbol. Preserve large color regions in the bottle. |
| [Craft](https://www.craft.do/brand) | Four compact colored forms, gentle layering and a light tile; minimal geometry can feel materially finished. |
| [Raycast](https://www.raycast.com/press) | Diagonal red geometry on a dark key-like tile conveys speed. The main mark survives after secondary lettering disappears. |
| [Arc](https://resources.arc.net/hc/en-us/articles/22352326167703-Change-Arc-for-Desktop-App-Icon) | Intersecting colored strokes and soft shadows make an abstract mark dimensional without photographic rendering. |
| [Ghostty](https://ghostty.org/) | A bold ghost within a terminal frame; large silhouette first, material detail second. Its [official configuration reference](https://ghostty.org/docs/config/reference#macos-icon) documents distinct frame/material layers and artist-created variants. |
| [Tower](https://www.git-tower.com/) | The installed icon simplifies a tower into a broad top, tapered glass body and striped base. Controlled perspective and large planes beat intricate architectural detail. |

**Size discipline:** at 128 px, judge shape, perspective and the RUSH wordmark; at 32 px, demand a separate dark cap, neck/shoulders, amber body and yellow label; at 16 px, expect only silhouette and color identity. Neither small-print sentence can realistically remain readable at 16/32 px. Check actual reductions, not just a zoomed-out 1024 preview. Apple's [Mac design session](https://developer.apple.com/videos/play/wwdc2019/809/) specifically discusses 16 px Finder icons and pixel hinting; its freeform-silhouette advice predates Tahoe's mask.

## Generation brief

Built-in image generation, one fresh generation per concept. Shared prompt: 1024×1024 PNG; true alpha outside a centered macOS squircle; one simplified semi-realistic amber bottle with broad dark cap and yellow label; 15–30° in-plane lean plus visible 3D tipping/foreshortening; smooth gradients, crisp silhouette, controlled upper-left lighting, gentle contact shadow. Original heavy italic red RUSH lettering with black outline and generous label margins. Exact supporting copy: `REPO ODOURISER` and `NOT FOR AGENT CONSUMPTION`. No copied commercial logo, photographic grain, studio floor, extra objects or text outside the label.

- **A — porcelain blue:** cool pale blue tile; cap leans upper-left about 22°, tipped toward the viewer with a visible broad cap ellipse; substantial but comfortably inset bottle; angular custom lettering.
- **B — graphite:** charcoal-to-slate tile; cap leans upper-right about 27°, base tipped toward the viewer; larger bottle and wider label; compact, very heavy speed lettering and a restrained warm rim highlight.
- **C — warm ivory:** soft ivory-to-peach tile; cap leans upper-left about 17°, more pronounced top-down perspective; smaller, stockier bottle with more breathing room; rounded heavy italic lettering with cut terminals. Additional prompt emphasis: broad sculpted color planes, matte label, minimal cap flutes and no glass caustics.

## Result review

**Pick C (`rush-icon-v2-c.png`).** Its broad cap flutes, quieter amber shading and warm tile make it the least photographic. The cap ellipse and shortened shoulders establish depth, while the diagonal remains clear. Its angular, connected wordmark has personality and stays inside the label. A is the runner-up for cool/warm contrast; B has the strongest motion but remains too close to a glossy product render.

| Result | 128 px | 32 px | 16 px | Lettering / clipping |
|---|---|---|---|---|
| A | RUSH is readable; broad top ellipse communicates tilt. Cap ribs remain busy. | Clearly a bottle, with separated cap and label. | Bottle/color identity survives; text does not. | All three strings appear correctly spelled at full size. H is contained; left R margin is tighter than the requested 10%. No bottle cropping. |
| B | Strongest diagonal and largest wordmark; base ellipse visibly tips forward. | Bottle is clear, although dark cap competes with tile. | Yellow/red diagonal survives; cap is less distinct. | Both small-print sentences and RUSH are correct. H is fully inside but closer to the right edge than ideal. No bottle cropping. |
| C | Clear RUSH, simpler cap, strongest illustration feel. | Best separation of dark cap, amber shoulders and yellow label. | Compact bottle silhouette survives; lettering collapses to color. | All wording is correct, including ODOURISER. No letter or bottle clipping. The lettering has pointed terminals rather than the fully rounded treatment requested. |

Inspected native 16/32/128 px reductions as well as full-size output. All three final files are 1024×1024 RGBA PNGs with actual transparent exterior regions, not painted checkerboards. Generation returned 1254 px images; final sizing used macOS `sips`, retaining alpha. The tile sizes are approximate rather than exact template geometry.

**Unresolved edge defect:** the generator left stray translucent/colored fringe pixels outside the intended squircle, most noticeable on A and C against dark backgrounds. Built-in cleanup edits requested only a smooth alpha contour and no external pixels; C's cleanup helped but did not eliminate the fringe. A's cleanup was not an improvement, so its original generation was retained. B has less visible fringing, but some nominally empty pixels have tiny nonzero alpha. Thus these are useful design candidates, not perfectly clean production masks. No spelling or clipping errors were found; the exterior-alpha defect remains. A/B also retain more realistic reflections than the ideal brief, and C filled more of the tile than its initial 70% target.
