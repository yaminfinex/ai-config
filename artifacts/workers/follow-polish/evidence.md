# Follow polish live evidence

Captured 2026-08-28 from the `follow-polish` worktree binary served on
`http://127.0.0.1:4493` with `AI_CONFIG_ROOT` unset. The owner serve on port
4400 was not touched.

## Build identity

The first `/api/events?agents=impl-nedo` SSE event was:

```text
event: hello
data: {"buildIdentity":"executable:8f65ab04171b17707c790c0782fd8c101e7ddb9547375197ba6880666476d5f1"}
```

## Switch geometry

Measured at a 1440 × 900 viewport with `getBoundingClientRect()` before and
after switching the same `impl-nedo` agent tab:

| View | x | y | width | height |
| --- | ---: | ---: | ---: | ---: |
| Transcript | 1157.46875 | 38 | 117.484375 | 25 |
| Screen | 1157.46875 | 38 | 117.484375 | 25 |

Every value is identical: observed layout shift is zero.

## Follow / jump behavior

| View | At bottom | After scroll-up | After Jump to bottom |
| --- | --- | --- | --- |
| Transcript | `scrollTop=1179`, max `1179` | `scrollTop=0`, button visible | `scrollTop=1179`, button absent |
| Screen | `scrollTop=691`, max `691` | `scrollTop=0`, button visible | `scrollTop=691`, button absent |

Screenshots:

- Transcript: `light-transcript-jump.png`, `dark-transcript-jump.png`
- Screen: `light-screen-jump.png`, `dark-screen-jump.png`

## Terminal label

The board and sidebar show the short label **Terminal** while retaining the
separate unattributed status/warning semantics:

- `light-terminal-label-board.png`
- `dark-terminal-label-board.png`

## Transcript modes and pill colors

The `impl-nedo` tab captures each active mode in both themes:

- Compact: `light-compact-colored-pills.png`, `dark-compact-colored-pills.png`
- Normal: `light-normal.png`, `dark-normal.png`
- Full: `light-full.png`, `dark-full.png`

The `ziru` compact transcript captures all four semantic pill tones together
in both themes: tool (`Bash`, blue), thinking (purple), bus message
(`impl-nedo`, green), and other (`task`, amber):

- `light-compact-pill-variety.png`
- `dark-compact-pill-variety.png`

Automated theme tests independently measure every pill text/background token
pair in both themes; the lowest contrast ratio is 6.61:1 (WCAG AA requires
4.5:1 for this text size).
