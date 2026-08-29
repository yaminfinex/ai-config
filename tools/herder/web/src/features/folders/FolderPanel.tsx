import { useEffect, useState } from 'react'
import { useQuery } from '@tanstack/react-query'
import { apiProblem, getBacklog, getFileTree, queryKeys } from '../../api/client'
import type { BacklogRead, FileTarget, FileTreeEntry, FolderTarget } from '../../types'
import { Banner } from '../../shared/presentation'
import { FilePanel } from '../files/FilePanel'
import { isMarkdownPath, type FileViewMode } from '../files/fileTabs'
import { rootLabel } from '../files/fileResolution'
import { boardColumns, taskFileTarget } from './folderModel'
import { initialGitFileState } from '../git/gitViewModel'

function childPath(parent: string, name: string) {
  return [parent.replace(/\/+$/u, ''), name].filter(Boolean).join('/')
}

function boardAvailable(value: Awaited<ReturnType<typeof getBacklog>> | undefined): value is BacklogRead {
  return Boolean(value && 'statuses' in value)
}

function DirectoryTree({ root, path, depth, currentDir, onDirectory, onSelect, onOpenFile }: {
  root: string
  path: string
  depth: number
  currentDir: string
  onDirectory: (path: string) => void
  onSelect: (target: FileTarget) => void
  onOpenFile: (target: FileTarget) => void
}) {
  const [expanded, setExpanded] = useState(depth === 0)
  const tree = useQuery({
    queryKey: queryKeys.fileTree(root, path),
    queryFn: ({ signal }) => getFileTree(root, path, fetch, signal),
    enabled: expanded,
    retry: false,
  })
  const entries = tree.data?.entries ?? []
  const looksLikeBoard = entries.some((entry) => entry.kind === 'file' && entry.name === 'config.yml') &&
    entries.some((entry) => entry.kind === 'directory' && entry.name === 'tasks')
  const title = path ? rootLabel(path) : rootLabel(root)
  return <div className="folder-tree-node">
    <div className={`folder-tree-row directory${currentDir === path ? ' current' : ''}`} style={{ paddingLeft: depth * 13 }}>
      <button type="button" className={`folder-disclosure${expanded ? ' expanded' : ''}`} aria-label={`${expanded ? 'Collapse' : 'Expand'} ${title}`} onClick={() => setExpanded((value) => !value)}>›</button>
      <button type="button" className="folder-entry-name" title={path || root} onClick={() => { setExpanded(true); onDirectory(path) }}><span aria-hidden="true">▰</span>{title}</button>
      {looksLikeBoard && <button type="button" className="folder-row-action" onClick={() => onDirectory(path)}>Board</button>}
    </div>
    {expanded && <div className="folder-tree-children">
      {tree.isPending && <div className="folder-tree-state" style={{ paddingLeft: (depth + 1) * 13 }}>Loading…</div>}
      {tree.error && <div className="folder-tree-error" role="alert" style={{ marginLeft: (depth + 1) * 13 }}>{tree.error.message}</div>}
      {tree.data && entries.length === 0 && <div className="folder-tree-state" style={{ paddingLeft: (depth + 1) * 13 }}>Empty folder</div>}
      {entries.map((entry) => entry.kind === 'directory'
        ? <DirectoryTree root={root} path={childPath(path, entry.name)} depth={depth + 1} currentDir={currentDir} onDirectory={onDirectory} onSelect={onSelect} onOpenFile={onOpenFile} key={entry.name} />
        : <TreeFile root={root} parent={path} entry={entry} depth={depth + 1} onSelect={onSelect} onOpenFile={onOpenFile} key={entry.name} />)}
    </div>}
  </div>
}

function TreeFile({ root, parent, entry, depth, onSelect, onOpenFile }: {
  root: string
  parent: string
  entry: FileTreeEntry
  depth: number
  onSelect: (target: FileTarget) => void
  onOpenFile: (target: FileTarget) => void
}) {
  const target = { root, path: childPath(parent, entry.name) }
  if (entry.kind === 'symlink') return <div className="folder-tree-row symlink" style={{ paddingLeft: depth * 13 }} title="Symlink is not opened from the tree"><span className="folder-tree-spacer" /><span aria-hidden="true">↗</span><span className="folder-entry-static">{entry.name}</span></div>
  return <div className="folder-tree-row file" style={{ paddingLeft: depth * 13 }}>
    <span className="folder-tree-spacer" />
    <button type="button" className="folder-entry-name" title={`Preview ${target.path}`} onClick={() => onSelect(target)} onDoubleClick={() => onOpenFile(target)}><span aria-hidden="true">◇</span>{entry.name}</button>
    <button type="button" className="folder-row-action" title={`Open ${entry.name} as a dock tab`} aria-label={`Open ${entry.name} as a tab`} onClick={() => onOpenFile(target)}>Open tab</button>
  </div>
}

function BacklogBoard({ backlog, onOpenFile }: { backlog: BacklogRead, onOpenFile: (target: FileTarget) => void }) {
  return <section className="backlog-board" aria-label={`Backlog board for ${backlog.path || rootLabel(backlog.root)}`}>
    {backlog.truncated && <div className="truncation-banner">This board is truncated at 2,000 task files. Additional tasks are not shown.</div>}
    <div className="backlog-columns">
      {boardColumns(backlog).map((column) => <section className={`backlog-column${column.overflow ? ' overflow' : ''}`} key={column.status}>
        <header><strong>{column.status}</strong><span>{column.tasks.length}</span></header>
        <div className="backlog-cards">
          {column.tasks.length === 0 && <p>Empty</p>}
          {column.tasks.map((task) => <button type="button" className="backlog-card" key={task.file} onClick={() => onOpenFile(taskFileTarget(backlog.root, backlog.path, task.file))}>
            <span className="backlog-card-title">{task.title || task.id || task.file}</span>
            {task.id && task.title && <span className="backlog-card-id">{task.id}</span>}
            {(task.priority || task.labels?.length || task.assignee?.length) && <span className="backlog-card-meta">
              {task.priority && <span className="backlog-chip priority">{task.priority}</span>}
              {task.labels?.map((label) => <span className="backlog-chip" key={label}>{label}</span>)}
              {task.assignee?.map((assignee) => <span className="backlog-chip assignee" key={assignee}>@{assignee}</span>)}
            </span>}
          </button>)}
        </div>
      </section>)}
    </div>
    {backlog.unparsed.length > 0 && <details className="backlog-quarantine"><summary>{backlog.unparsed.length} unparsed task file{backlog.unparsed.length === 1 ? '' : 's'}</summary>
      <ul>{backlog.unparsed.map((item) => <li key={item.file}><strong>{item.file}</strong><span>{item.reason}</span></li>)}</ul>
    </details>}
  </section>
}

export function FolderPanel({ target, active, onOpenFile, onOpenFolder }: {
  target: FolderTarget
  active: boolean
  onOpenFile: (target: FileTarget) => void
  onOpenFolder: (target: FolderTarget) => void
}) {
  const [treeHidden, setTreeHidden] = useState(false)
  const [currentDir, setCurrentDir] = useState(target.path)
  const [selected, setSelected] = useState<FileTarget | null>(null)
  const [viewMode, setViewMode] = useState<FileViewMode>('source')
  const [gitState, setGitState] = useState(initialGitFileState)
  const [boardView, setBoardView] = useState(true)
  const currentTree = useQuery({
    queryKey: queryKeys.fileTree(target.root, currentDir),
    queryFn: ({ signal }) => getFileTree(target.root, currentDir, fetch, signal),
    retry: false,
  })
  const backlog = useQuery({
    queryKey: queryKeys.backlog(target.root, currentDir),
    queryFn: ({ signal }) => getBacklog(target.root, currentDir, fetch, signal),
    retry: false,
  })
  const available = boardAvailable(backlog.data)
  const showBoard = available && boardView

  useEffect(() => {
    setCurrentDir(target.path)
    setSelected(null)
    setBoardView(true)
  }, [target.path, target.root])

  useEffect(() => {
    if (selected || !currentTree.data) return
    const manifest = currentTree.data.entries.find((entry) => entry.kind === 'file' && entry.name.toLowerCase() === 'mission.md')
    if (manifest) {
      setSelected({ root: target.root, path: childPath(currentDir, manifest.name) })
      setViewMode('rendered')
    }
  }, [currentDir, currentTree.data, selected, target.root])

  const chooseDirectory = (path: string) => {
    setCurrentDir(path)
    setSelected(null)
    setBoardView(true)
  }
  const chooseFile = (file: FileTarget) => {
    setSelected(file)
    setViewMode(isMarkdownPath(file.path) && !file.line ? 'rendered' : 'source')
    setGitState(initialGitFileState())
    setBoardView(false)
  }
  const backlogFailure = backlog.error ? apiProblem(backlog.error) : null
  return <main className="folder-panel">
    <header className="folder-header">
      <div className="folder-title"><strong>{rootLabel(currentDir) || rootLabel(target.root)}</strong><span>{currentDir || '.'}</span><span className="root-path" title={target.root}>{target.root}</span></div>
      <button type="button" onClick={() => setTreeHidden((value) => !value)}>{treeHidden ? 'Show tree' : 'Hide tree'}</button>
      {available && <div className="detail-toggle folder-view-toggle" aria-label="Folder view">
        <button type="button" className={!showBoard ? 'active' : ''} aria-pressed={!showBoard} onClick={() => setBoardView(false)}>Files</button>
        <button type="button" className={showBoard ? 'active' : ''} aria-pressed={showBoard} onClick={() => setBoardView(true)}>Board</button>
      </div>}
      <button type="button" onClick={() => { currentTree.refetch(); backlog.refetch() }} disabled={currentTree.isFetching || backlog.isFetching}>{currentTree.isFetching || backlog.isFetching ? 'Refreshing…' : 'Refresh'}</button>
    </header>
    {backlogFailure && <Banner source="backlog" detail={`${backlogFailure.problem.error}: ${backlogFailure.problem.detail}`} />}
    <div className={`folder-workspace${treeHidden ? ' tree-hidden' : ''}`}>
      {!treeHidden && <aside className="folder-tree" aria-label="Folder tree"><DirectoryTree root={target.root} path={target.path} depth={0} currentDir={currentDir} onDirectory={chooseDirectory} onSelect={chooseFile} onOpenFile={onOpenFile} /></aside>}
      <section className="folder-detail">
        {showBoard && backlog.data && boardAvailable(backlog.data) ? <>
          <div className="backlog-facts"><span>Fetched {new Date(backlog.data.fetched_at).toLocaleString()}</span><span>{backlog.data.tasks.length} parsed tasks</span></div>
          <BacklogBoard backlog={backlog.data} onOpenFile={onOpenFile} />
        </> : selected ? <FilePanel target={selected} viewMode={viewMode} gitState={gitState} active={active} onViewMode={setViewMode} onGitState={setGitState} onOpenFile={onOpenFile} onOpenFolder={onOpenFolder} />
          : <div className="folder-empty" role="status"><strong>Select a file</strong><p>A single click previews it here. Double-click or use Open tab to create a dock preview.</p></div>}
      </section>
    </div>
  </main>
}
