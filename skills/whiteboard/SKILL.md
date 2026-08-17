---
name: whiteboard
description: Live terminal whiteboard — maintain a hot-reloading presenterm deck in a herdr side pane while you work, with diagrams rendered as real images via kitty graphics. Use when the user says "open a whiteboard", "whiteboard this", "show me in a pane", "sketch in the terminal", "draw as you go", or wants live diagrams beside the session without the browser canvas.
---

# whiteboard

One markdown file, one side pane. You edit the file, presenterm hot-reloads it
on every save, and the human watches diagrams appear next to the session. Keep
the board current as your thinking evolves — it is a live surface, not a report.

## Steps

1. Preflight: `command -v presenterm mmdc d2 rsvg-convert jq` all resolve and
   `$HERDR_PANE_ID` is set. Anything missing → `references/setup.md`. Outside
   herdr, keep diagrams inline in chat as d2 ASCII (`d2 g.d2 g.txt`) and stop
   here.
2. Create the board and write a first slide *before* opening the pane, so it
   never renders empty:

   ```bash
   wb=$(mktemp -d /tmp/whiteboard.XXXX)
   # write $wb/board.md, then:
   pane=$(herdr pane split --current --direction right --ratio 0.45 --no-focus | jq -r .result.pane.pane_id)
   herdr pane run "$pane" "presenterm $wb/board.md"
   ```

3. Tell the human once: ←/→ moves slides; navigation is theirs, the pane id is
   `$pane` if they want to resize or close it.
4. Update by rewriting the whole file; every save repaints the pane. Revise the
   current slide in place for the same topic; append a new slide for a new
   topic (reload keeps the viewer's slide position).
5. After the first diagram lands, ask the human to confirm they see a rendered
   image (not blank space). Blank → Symptoms.

## Authoring

- One idea per slide, `<!-- end_slide -->` between slides. A slide fits ~40
  terminal rows; split before it overflows.
- Start slides with a plain heading. Omit front-matter `title:` — it inserts an
  extra title slide.
- Mermaid renders natively:

  ````markdown
  ```mermaid +render +width:80%
  flowchart LR
      a --> b
  ```
  ````

- d2 lays out better than mermaid and has an ASCII twin for chat, but must be
  pre-rendered (its direct PNG export is broken upstream — see Symptoms):

  ```bash
  d2 "$wb/g.d2" "$wb/g.svg" && rsvg-convert -w 900 "$wb/g.svg" -o "$wb/g.png"   # image for the board
  d2 "$wb/g.d2" "$wb/g.txt"                                                     # ASCII for chat
  ```

  then embed `![](g.png)`.
- Graphviz for dense pure graphs: `dot -Tpng -Gdpi=140 g.dot -o g.png`.
- Tables, code blocks with highlighting, and bold text all render — use them;
  the board beats prose only when it stays visual and terse.

## Symptoms

- Text renders but images are blank space → the human's connection is mosh, or
  their outer terminal lacks kitty graphics. Images need plain SSH plus a
  kitty-capable terminal (Ghostty, kitty, WezTerm) and herdr's
  `experimental.kitty_graphics` flag (`references/setup.md`). Until fixed,
  switch the board to d2 ASCII code blocks — those survive any transport.
- Every mermaid block errors → presenterm's mermaid config must point at a
  no-sandbox puppeteer JSON (`references/setup.md`).
- ```` ```d2 +render ```` inside presenterm fails with a Playwright 404 → d2's
  PNG export downloads a dead upstream driver; use the pre-render pipeline in
  Authoring instead.
