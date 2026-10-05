import { imageFailureReason } from '../../api/client'
import type { ImageLoad } from './imageViewModel'

// FileImage shows one same-origin image on a checkerboard so transparency
// shows. Fit caps it at the panel width without enlarging it; a click toggles
// fit and actual size. Load facts and failures go back to the panel.
export function FileImage({ src, mime, path, fit, onToggleFit, onLoad }: {
  src: string
  mime: string
  path: string
  fit: boolean
  onToggleFit: () => void
  onLoad: (update: (current: ImageLoad | null) => ImageLoad | null) => void
}) {
  return <div className="file-content file-image-content" role="region" aria-label={`Image ${path}`}>
    <div className={`file-image-stage${fit ? ' fit' : ''}`}>
      <button type="button" className="file-image-button" title={fit ? 'Show actual size' : 'Fit to panel'} aria-label={fit ? 'Show actual size' : 'Fit to panel'} onClick={onToggleFit}>
        <img className="file-image" src={src} alt={path}
          onLoad={(event) => {
            const { naturalWidth: width, naturalHeight: height } = event.currentTarget
            onLoad(() => ({ src, status: 'loaded', width, height }))
          }}
          onError={() => {
            onLoad(() => ({ src, status: 'error', reason: null }))
            void imageFailureReason(src, mime).then((reason) => onLoad((current) => current?.src === src && current.status === 'error' ? { src, status: 'error', reason } : current))
          }} />
      </button>
    </div>
  </div>
}
