import type { ITheme } from '@xterm/xterm'

export const terminalTheme = {
  foreground: '#d3d8e2', background: '#0d0f12', cursor: '#0d0f12', cursorAccent: '#0d0f12',
  selectionBackground: '#35526f', selectionForeground: '#f7f9fc', selectionInactiveBackground: '#293d52',
  black: '#0d0f12', red: '#ff7b72', green: '#7ee787', yellow: '#e3b341', blue: '#79c0ff', magenta: '#d2a8ff', cyan: '#56d4dd', white: '#d3d8e2',
  brightBlack: '#8b949e', brightRed: '#ffa198', brightGreen: '#aff5b4', brightYellow: '#f2cc60', brightBlue: '#a5d6ff', brightMagenta: '#e2c5ff', brightCyan: '#86e1e8', brightWhite: '#f7f9fc',
} as const satisfies ITheme

function luminance(hex: string) {
  const channels = [1, 3, 5].map((index) => Number.parseInt(hex.slice(index, index + 2), 16) / 255)
    .map((value) => value <= 0.04045 ? value / 12.92 : ((value + 0.055) / 1.055) ** 2.4)
  return 0.2126 * channels[0] + 0.7152 * channels[1] + 0.0722 * channels[2]
}

export function contrastRatio(foreground: string, background: string) {
  const foregroundLuminance = luminance(foreground)
  const backgroundLuminance = luminance(background)
  return (Math.max(foregroundLuminance, backgroundLuminance) + 0.05) / (Math.min(foregroundLuminance, backgroundLuminance) + 0.05)
}
