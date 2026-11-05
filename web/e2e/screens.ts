// The screens the presentation page is designed and checked on (design/present.md, "Screens"): the page's CSS pixels as a browser
// gives them, the browser's own bars taken off. The owner, the agent and the tests look at the same ones; the tap bar shows the
// size a screen has, to compare.

export type Screen = { name: string; width: number; height: number; scale: number; profile: string }

export const screens = {
  /** an iPad Pro 13" (M4) on its side, Safari full screen from the home screen */
  ipadLandscape: { name: "iPad Pro 13 landscape", width: 1376, height: 1032, scale: 2, profile: "handheld-landscape" },
  /** the same iPad held upright */
  ipadPortrait: { name: "iPad Pro 13 portrait", width: 1032, height: 1376, scale: 2, profile: "handheld-portrait" },
  /** a 27" Studio Display XDR at its default 2560×1440, the page in full screen */
  xdrFull: { name: "27in XDR full screen", width: 2560, height: 1440, scale: 2, profile: "desk" },
  /**
   * half of that display, Chrome tiled to one side (Window › Move & Resize › Left): 1280 wide, the height without the menu bar
   * (25) and Chrome's tabs and address bar (87); upright, yet a desk's window, not a tablet's (P-64)
   */
  xdrHalf: { name: "27in XDR half, Chrome", width: 1280, height: 1328, scale: 2, profile: "desk" },
} satisfies Record<string, Screen>
