import { createElement, memo, useMemo, type ReactNode } from 'react'
import ReactMarkdown, { defaultUrlTransform, type Components } from 'react-markdown'
import remarkGfm from 'remark-gfm'
import type { AgentMentionMatcher, AgentMentionOpen } from './agentMentions.ts'

const externalHTTP = /^https?:\/\//iu

export const fileMarkdownComponents = {
  a: ({ node, href = '', children, ...props }) => {
    void node
    return externalHTTP.test(href)
      ? createElement('a', { ...props, href, target: '_blank', rel: 'noopener noreferrer' }, children)
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

type AgentMarkdown = { matcher: AgentMentionMatcher, onOpen: AgentMentionOpen, sideHint?: string }

export function agentMarkdownOptions(matcher: AgentMentionMatcher, onOpen: AgentMentionOpen, sideHint?: string) {
  return { agentMentions: { matcher, onOpen, sideHint } }
}

export const Markdown = memo(function Markdown({ children, components, agentMentions }: { children: string, components?: Components, agentMentions?: AgentMarkdown }): ReactNode {
  const options = useMemo(() => {
    if (!agentMentions) return { remarkPlugins: [remarkGfm], components }
    const mentionComponents: Components = {
      ...components,
      a: ({ node, href = '', children: linkChildren, ...props }) => {
        void node
        if (!href.startsWith(agentScheme)) return createElement('a', { ...props, href }, linkChildren)
        let decoded: string
        try {
          decoded = decodeURIComponent(href.slice(agentScheme.length))
        } catch {
          return createElement('span', props, linkChildren)
        }
        const name = agentMentions.matcher.resolve(decoded)
        if (!name) return createElement('span', props, linkChildren)
        return createElement('button', {
          ...props,
          type: 'button',
          className: 'agent-mention',
          title: `Open ${name}${agentMentions.sideHint ? ` · ${agentMentions.sideHint}` : ''}`,
          onClick: (event) => agentMentions.onOpen(name, event),
        }, linkChildren)
      },
    }
    return {
      remarkPlugins: [remarkGfm, mentionPlugin(agentMentions.matcher)],
      components: mentionComponents,
      urlTransform: (url: string) => url.startsWith(agentScheme) ? url : defaultUrlTransform(url),
    }
  }, [agentMentions, components])
  return createElement(ReactMarkdown, { ...options, children })
})
