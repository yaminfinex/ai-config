# Dark mode (required in every artifact)

Always include dark mode: hand-rolled CSS variables on `:root` / `html.dark`, a small theme toggle button, `localStorage` persistence, and an apply-before-paint script in `<head>` (default to `prefers-color-scheme`).

Whenever the artifact contains SVG, style the SVG through CSS classes using those variables — never hard-coded hex inside the SVG — so it follows the theme.
