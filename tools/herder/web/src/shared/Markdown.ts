import { createElement, type ReactNode } from 'react'
import ReactMarkdown, { type Components } from 'react-markdown'
import remarkGfm from 'remark-gfm'

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

export function Markdown({ children, components }: { children: string, components?: Components }): ReactNode {
  return createElement(ReactMarkdown, { remarkPlugins: [remarkGfm], components, children })
}
