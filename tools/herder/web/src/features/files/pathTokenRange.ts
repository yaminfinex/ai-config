import { hasPathSignal, pathTokenSpanAt } from './fileResolution.ts'

type TextPoint = { node: Node, offset: number }
type Character = { value: string, point: TextPoint | null }

const structuralDelimiter = /[\s()[\]{}<>]/u
const isText = (node: Node) => node.nodeType === 3
const isBreak = (node: Node) => node.nodeName === 'BR'

// Text siblings and BRs share an offset map, so removing a wrap never changes
// the DOM coordinates used to select the original, visibly wrapped path.
export function pathTokenRangeAt(point: TextPoint, renderedCode = false, softWrap = true) {
  if (renderedCode || !softWrap) {
    const token = pathTokenSpanAt(point.node.textContent ?? '', point.offset, renderedCode)
    return { text: token.text, start: { node: point.node, offset: token.start }, end: { node: point.node, offset: token.end } }
  }
  let first = point.node
  while (first.previousSibling && (isText(first.previousSibling) || isBreak(first.previousSibling))) first = first.previousSibling
  const characters: Character[] = []
  let caret = 0
  for (let node: Node | null = first; node && (isText(node) || isBreak(node)); node = node.nextSibling) {
    if (isBreak(node)) {
      characters.push({ value: '\n', point: null })
      continue
    }
    const text = node.textContent ?? ''
    // ReactMarkdown emits BR followed by a formatting newline for one break.
    const startOffset = node.previousSibling && isBreak(node.previousSibling) && text.startsWith('\n') ? 1 : 0
    if (node === point.node) caret = characters.length + Math.max(0, point.offset - startOffset)
    for (let offset = startOffset; offset < text.length; offset++) characters.push({ value: text[offset], point: { node, offset } })
  }
  let text = characters.map((character) => character.value).join('')
  for (let index = 1; index < characters.length - 1; index++) {
    if (text[index] !== '\n' || structuralDelimiter.test(text[index - 1]) || structuralDelimiter.test(text[index + 1])) continue
    const joined = text.slice(0, index) + text.slice(index + 1)
    if (!hasPathSignal(pathTokenSpanAt(joined, index).text, false)) continue
    characters.splice(index, 1)
    if (caret > index) caret--
    text = joined
    index--
  }
  const token = pathTokenSpanAt(text, caret)
  const start = characters[token.start]?.point ?? point
  const last = characters[token.end - 1]?.point
  return { text: token.text, start, end: last ? { node: last.node, offset: last.offset + 1 } : point }
}
