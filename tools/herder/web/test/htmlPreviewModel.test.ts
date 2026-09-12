import assert from 'node:assert/strict'
import test from 'node:test'
import { htmlPreviewModel, type HtmlRawState } from '../src/features/files/htmlPreviewModel.ts'

for (const isHtml of [false, true]) {
  for (const truncated of [false, true]) {
    for (const rawState of ['idle', 'loading', 'success', 'error'] as HtmlRawState[]) {
      test(`HTML preview model: html=${isHtml} truncated=${truncated} raw=${rawState}`, () => {
        const got = htmlPreviewModel(isHtml, truncated, rawState, '716,800 bytes')
        assert.equal(got.renderedEnabled, true)
        assert.equal(got.srcdocSource, isHtml && truncated ? 'raw' : 'content')
        assert.equal(got.banner, isHtml && truncated && rawState === 'success'
          ? 'Rendered from the full 716,800 bytes. The source view shows the first 256 KiB.'
          : null)
      })
    }
  }
}
