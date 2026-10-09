import { createContext, createElement, memo, useContext, type ComponentPropsWithoutRef, type MouseEvent, type ReactNode } from 'react'
import ReactMarkdown, { defaultUrlTransform, type Components, type Options } from 'react-markdown'
import remarkGfm from 'remark-gfm'
import type { AgentMentionMatcher, AgentMentionOpen } from './agentMentions.ts'
import { isLocalHref } from './pathHref.ts'
import { FencedBlock } from './CodeBlock.ts'

const externalHTTP = /^https?:\/\//iu
const inlineLinkClass = (className?: string) => ['inline-link', className].filter(Boolean).join(' ')

// Fenced blocks and tables share one look in transcripts and FilePanel (brief decisions 1 and 4).
export const blockComponents = {
  pre: FencedBlock,
  table: ({ node, children, ...props }) => {
    void node
    return createElement('div', { className: 'table-scroll' }, createElement('table', props, children))
  },
} satisfies Components

export const fileMarkdownComponents = {
  ...blockComponents,
  a: ({ node, href = '', children, className, ...props }) => {
    void node
    return externalHTTP.test(href)
      ? createElement('a', { ...props, className: inlineLinkClass(className), href, target: '_blank', rel: 'noopener noreferrer' }, children)
      : createElement('span', { className: 'markdown-relative-link', title: `Relative link: ${href}` }, children, ' ', createElement('code', null, href || 'target unavailable'))
  },
  img: ({ node, src = '', alt = '', ...props }) => {
    void node
    return externalHTTP.test(src)
      ? createElement('img', { ...props, src, alt, loading: 'lazy' })
      : createElement('span', { className: 'markdown-image-stub', role: 'img', 'aria-label': `${alt || 'Image'} (${src || 'target unavailable'})` },
        createElement('span', null, alt || 'Image'), createElement('code', null, src || 'target unavailable'))
  },
} satisfies Components

const agentScheme = 'herder-agent:'
const skippedMentionParents = new Set(['code', 'inlineCode', 'link', 'linkReference', 'html'])

type MarkdownNode = { type: string, value?: string, children?: MarkdownNode[] }

const voidHtmlElements = new Set(['area', 'base', 'br', 'col', 'embed', 'hr', 'img', 'input', 'link', 'meta', 'param', 'source', 'track', 'wbr'])

function htmlDepthChange(value: string) {
  let change = 0
  for (const match of value.matchAll(/<(\/?)([A-Za-z][^\s/>]*)(?:\s[^>]*)?(\/?)>/gu)) {
    const name = match[2].toLowerCase()
    if (match[1]) change--
    else if (!match[3] && !voidHtmlElements.has(name)) change++
  }
  return change
}

function mentionPlugin(matcher: AgentMentionMatcher) {
  return () => (tree: MarkdownNode) => {
    const transform = (parent: MarkdownNode) => {
      if (skippedMentionParents.has(parent.type) || !parent.children) return
      const children: MarkdownNode[] = []
      let rawHtmlDepth = 0
      parent.children.forEach((child) => {
        if (child.type === 'html') {
          rawHtmlDepth = Math.max(0, rawHtmlDepth + htmlDepthChange(child.value ?? ''))
          children.push(child)
          return
        }
        if (rawHtmlDepth > 0) {
          children.push(child)
          return
        }
        if (child.type !== 'text' || !child.value) {
          transform(child)
          children.push(child)
          return
        }
        const tokens = matcher.tokenize(child.value)
        if (!tokens.some((token) => typeof token !== 'string')) {
          children.push(child)
          return
        }
        tokens.forEach((token) => children.push(typeof token === 'string'
          ? { type: 'text', value: token }
          : { type: 'link', url: `${agentScheme}${encodeURIComponent(token.name)}`, children: [{ type: 'text', value: token.text }] } as MarkdownNode))
      })
      parent.children = children
    }
    transform(tree)
  }
}

const skippedBreakParents = new Set(['code', 'inlineCode', 'html'])

// Chat messages keep the writer's line breaks: a single newline inside a
// paragraph becomes a hard break, as remark-breaks does.
function lineBreaksPlugin() {
  return (tree: MarkdownNode) => {
    const transform = (parent: MarkdownNode) => {
      if (skippedBreakParents.has(parent.type) || !parent.children) return
      parent.children = parent.children.flatMap((child) => {
        if (child.type !== 'text' || !child.value?.includes('\n')) {
          transform(child)
          return [child]
        }
        return child.value.split(/\r?\n/).flatMap((part, index) => index === 0 ? [{ type: 'text', value: part }] : [{ type: 'break' }, { type: 'text', value: part }])
      })
    }
    transform(tree)
  }
}

type AgentMarkdown = { matcher: AgentMentionMatcher, onOpen: AgentMentionOpen, sideHint?: string }

export function agentMarkdownOptions(matcher: AgentMentionMatcher, onOpen: AgentMentionOpen, sideHint?: string) {
  return { agentMentions: { matcher, onOpen, sideHint } }
}

// The open handler and side hint reach mention buttons through context, so a
// rendered tree depends only on its text, matcher, components and line breaks
// and can be reused by every panel that shows it.
const MentionHandlers = createContext<AgentMarkdown | null>(null)

function MentionLink({ href, className, children, ...props }: ComponentPropsWithoutRef<'a'> & { href: string }) {
  const mentions = useContext(MentionHandlers)
  if (!mentions || !href.startsWith(agentScheme)) {
    if (isLocalHref(href)) return createElement('span', { className: 'path-link', title: href }, children)
    return createElement('a', {
      ...props, className: inlineLinkClass(className), href,
      ...(externalHTTP.test(href) ? { target: '_blank', rel: 'noopener noreferrer' } : {}),
    }, children)
  }
  let decoded: string
  try {
    decoded = decodeURIComponent(href.slice(agentScheme.length))
  } catch {
    return createElement('span', props, children)
  }
  const name = mentions.matcher.resolve(decoded)
  if (!name) return createElement('span', props, children)
  return createElement('button', {
    ...props,
    type: 'button',
    className: 'inline-link agent-mention',
    title: `Open ${name}${mentions.sideHint ? ` · ${mentions.sideHint}` : ''}`,
    onClick: (event: MouseEvent<HTMLButtonElement>) => mentions.onOpen(name, event),
  }, children)
}

const mentionLink: Components['a'] = ({ node, href = '', ...props }) => {
  void node
  return createElement(MentionLink, { ...props, href })
}
const mentionURLTransform = (url: string) => url.startsWith(agentScheme) || isLocalHref(url) ? url : defaultUrlTransform(url)

type RenderOptions = Pick<Options, 'remarkPlugins' | 'components' | 'urlTransform'>

// One options object per matcher, components and line-break setting, so the
// rendered-tree cache below can key on its identity.
const noMatcher = {}
const noComponents = {}
const renderOptions = new WeakMap<object, WeakMap<object, [RenderOptions | undefined, RenderOptions | undefined]>>()

export function markdownRenderOptions(matcher: AgentMentionMatcher | undefined, components: Components | undefined, lineBreaks: boolean): RenderOptions {
  let byComponents = renderOptions.get(matcher ?? noMatcher)
  if (!byComponents) renderOptions.set(matcher ?? noMatcher, byComponents = new WeakMap())
  let pair = byComponents.get(components ?? noComponents)
  if (!pair) byComponents.set(components ?? noComponents, pair = [undefined, undefined])
  const slot = lineBreaks ? 1 : 0
  const cached = pair[slot]
  if (cached) return cached
  const breaks = lineBreaks ? [lineBreaksPlugin] : []
  const options: RenderOptions = matcher
    ? { remarkPlugins: [remarkGfm, mentionPlugin(matcher), ...breaks], components: { ...blockComponents, ...components, a: mentionLink }, urlTransform: mentionURLTransform }
    : { remarkPlugins: [remarkGfm, ...breaks], components: { ...blockComponents, ...components } }
  pair[slot] = options
  return options
}

// A bounded cache of rendered markdown, most recently used last. A remounted
// transcript (a space switch, a reopened tab) reuses the parse instead of
// running remark again for every entry. Bounded by source length.
const renderCacheChars = 2_000_000
const rendered = new Map<string, WeakMap<RenderOptions, ReactNode>>()
let renderedChars = 0

export function renderMarkdown(options: RenderOptions, text: string): ReactNode {
  let byOptions = rendered.get(text)
  if (byOptions) rendered.delete(text)
  else {
    byOptions = new WeakMap()
    renderedChars += text.length
  }
  rendered.set(text, byOptions)
  for (const oldest of rendered.keys()) {
    if (renderedChars <= renderCacheChars || oldest === text) break
    rendered.delete(oldest)
    renderedChars -= oldest.length
  }
  let node = byOptions.get(options)
  if (node === undefined) byOptions.set(options, node = ReactMarkdown({ ...options, children: text }))
  return node
}

export const Markdown = memo(function Markdown({ children, components, agentMentions, lineBreaks = false }: { children: string, components?: Components, agentMentions?: AgentMarkdown, lineBreaks?: boolean }): ReactNode {
  const node = renderMarkdown(markdownRenderOptions(agentMentions?.matcher, components, lineBreaks), children)
  return agentMentions ? createElement(MentionHandlers.Provider, { value: agentMentions }, node) : node
})
