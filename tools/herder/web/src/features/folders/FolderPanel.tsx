import { useEffect, useState, type KeyboardEvent } from 'react'
import { useQuery } from '@tanstack/react-query'
import { getBacklog, getFileTree, queryKeys } from '../../api/client'
import type { BacklogRead, FileTarget, FileTreeEntry, FolderTarget } from '../../types'
import { Banner } from '../../shared/presentation'
import { FilePanel } from '../files/FilePanel'
import { isMarkdownPath, type FileViewMode } from '../files/fileTabs'
import { rootLabel } from '../files/fileResolution'
import { boardColumns, folderSelectionTarget, parentFolderPath, rootJoinedAbsolutePath, taskFileTarget } from './folderModel'
import { initialGitFileState } from '../git/gitViewModel'
import { openInSideLabel, placementFromModifiers, type OpenPlacement } from '../layout/openPlacement'
import { useFileWatch } from '../../stream/fileWatchRegistry'
import { failureBanner, PanelState } from '../../shared/PanelState'
import { TreeRow, TreeState } from '../../shared/TreeRow'
import { treeChildIndex, treeKeyIntent, treeParentIndex } from '../../shared/treeModel'
import { PathCopyButton } from '../../shared/PathCopyButton'

function childPath(parent: string, name: string) {
  return [parent.replace(/\/+$/u, ''), name].filter(Boolean).join('/')
}

function boardAvailable(value: Awaited<ReturnType<typeof getBacklog>> | undefined): value is BacklogRead {
  return Boolean(value && 'statuses' in value)
}

function focusTreeRow(rows: HTMLElement[], index: number) {
  const target = rows[index]
  if (!target) return
  rows.forEach((row) => { row.tabIndex = row === target ? 0 : -1 })
  target.focus()
}

function handleTreeKeyDown(event: KeyboardEvent<HTMLElement>) {
  if (event.altKey || event.ctrlKey || event.metaKey) return
  const row = (event.target as Element).closest<HTMLElement>('[role="treeitem"]')
  if (!row || !event.currentTarget.contains(row)) return
  const expandable = row.hasAttribute('aria-expanded')
  const expanded = row.getAttribute('aria-expanded') === 'true'
  const intent = treeKeyIntent(event.key, expandable, expanded)
  if (!intent || (intent === 'primary' && event.target !== row)) return
  const rows = [...event.currentTarget.querySelectorAll<HTMLElement>('[role="treeitem"]')]
  const index = rows.indexOf(row)
  const levels = rows.map((item) => Number(item.getAttribute('aria-level')) || 1)
  event.preventDefault()
  if (intent === 'first') focusTreeRow(rows, 0)
  else if (intent === 'last') focusTreeRow(rows, rows.length - 1)
  else if (intent === 'previous') focusTreeRow(rows, index - 1)
  else if (intent === 'next') focusTreeRow(rows, index + 1)
  else if (intent === 'child') focusTreeRow(rows, treeChildIndex(levels, index))
  else if (intent === 'parent') focusTreeRow(rows, treeParentIndex(levels, index))
  else if (intent === 'expand' || intent === 'collapse') row.querySelector<HTMLElement>('.tree-disclosure')?.click()
  else row.querySelector<HTMLElement>('[data-tree-primary]')?.click()
}

function DirectoryTree({ root, path, depth, currentDir, selectedFile, onDirectory, onSelect, onOpenFile, onOpenFolder }: {
  root: string
  path: string
  depth: number
  currentDir: string
  selectedFile: FileTarget | null
  onDirectory: (path: string) => void
  onSelect: (target: FileTarget) => void
  onOpenFile: (target: FileTarget, placement?: OpenPlacement) => void
  onOpenFolder: (target: FolderTarget, placement?: OpenPlacement) => void
}) {
  const [expanded, setExpanded] = useState(depth === 0)
  useFileWatch({ kind: 'folder', root, path }, expanded)
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
  const sideHint = openInSideLabel(navigator.userAgent)
  return <div className="folder-tree-node" role="none">
    <TreeRow depth={depth} name={title} expandable expanded={expanded} selected={currentDir === path} className="directory" icon={<span>▰</span>}
      itemProps={{ role: 'treeitem', 'aria-level': depth + 1, tabIndex: depth === 0 ? 0 : -1 }}
      onToggle={() => setExpanded((value) => !value)}
      label={<button type="button" className="tree-primary folder-entry-name" data-tree-primary title={`${path || root} · ${sideHint}`} onClick={(event) => {
        if (event.altKey) onOpenFolder({ root, path }, placementFromModifiers(event))
        else { setExpanded(true); onDirectory(path) }
      }}>{title}</button>}
      trailing={looksLikeBoard && <button type="button" className="folder-row-action" onClick={() => onDirectory(path)}>Board</button>} />
    {expanded && <div className="folder-tree-children" role="group">
      {tree.isPending && <TreeState depth={depth + 1} title="Loading…" />}
      {tree.error && <TreeState depth={depth + 1} role="alert" title="Folder unavailable" detail={tree.error.message} />}
      {tree.data && entries.length === 0 && <TreeState depth={depth + 1} title="Empty folder" />}
      {entries.map((entry) => entry.kind === 'directory'
        ? <DirectoryTree root={root} path={childPath(path, entry.name)} depth={depth + 1} currentDir={currentDir} selectedFile={selectedFile} onDirectory={onDirectory} onSelect={onSelect} onOpenFile={onOpenFile} onOpenFolder={onOpenFolder} key={entry.name} />
        : <TreeFile root={root} parent={path} entry={entry} depth={depth + 1} selected={selectedFile?.path === childPath(path, entry.name)} onSelect={onSelect} onOpenFile={onOpenFile} key={entry.name} />)}
    </div>}
  </div>
}

function TreeFile({ root, parent, entry, depth, selected, onSelect, onOpenFile }: {
  root: string
  parent: string
  entry: FileTreeEntry
  depth: number
  selected: boolean
  onSelect: (target: FileTarget) => void
  onOpenFile: (target: FileTarget, placement?: OpenPlacement) => void
}) {
  const target = { root, path: childPath(parent, entry.name) }
  if (entry.kind === 'symlink') return <TreeRow depth={depth} name={entry.name} className="symlink" icon={<span>↗</span>} label={<span className="folder-entry-static">{entry.name}</span>}
    title="Symlink is not opened from the tree" itemProps={{ role: 'treeitem', 'aria-level': depth + 1, tabIndex: -1 }} />
  return <TreeRow depth={depth} name={entry.name} selected={selected} className="file" icon={<span>◇</span>} itemProps={{ role: 'treeitem', 'aria-level': depth + 1, tabIndex: -1 }}
    label={<button type="button" className="tree-primary folder-entry-name" data-tree-primary title={`Preview ${target.path} · ${openInSideLabel(navigator.userAgent)}`} onClick={(event) => {
      if (event.altKey) onOpenFile(target, placementFromModifiers(event))
      else onSelect(target)
    }} onDoubleClick={(event) => onOpenFile(target, placementFromModifiers(event))}>{entry.name}</button>}
    trailing={<button type="button" className="folder-row-action" title={`Open ${entry.name} as a dock tab · ${openInSideLabel(navigator.userAgent)}`} aria-label={`Open ${entry.name} as a tab`} onClick={(event) => onOpenFile(target, placementFromModifiers(event))}>Open tab</button>} />
}

function BacklogBoard({ backlog, onOpenFile }: { backlog: BacklogRead, onOpenFile: (target: FileTarget, placement?: OpenPlacement) => void }) {
  return <section className="backlog-board" aria-label={`Backlog board for ${backlog.path || rootLabel(backlog.root)}`}>
    {backlog.truncated && <div className="truncation-banner">This board is truncated at 2,000 task files. Additional tasks are not shown.</div>}
    <div className="backlog-columns">
      {boardColumns(backlog).map((column) => <section className={`backlog-column${column.overflow ? ' overflow' : ''}`} key={column.status}>
        <header><strong>{column.status}</strong><span>{column.tasks.length}</span></header>
        <div className="backlog-cards">
          {column.tasks.length === 0 && <p>Empty</p>}
          {column.tasks.map((task) => <button type="button" className="backlog-card" title={openInSideLabel(navigator.userAgent)} key={task.file} onClick={(event) => onOpenFile(taskFileTarget(backlog.root, backlog.path, task.file), placementFromModifiers(event))}>
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

export function FolderPanel({ target, agents, active, selectionHint, onSelectionHintConsumed, onOpenFile, onOpenFolder }: {
  target: FolderTarget
  agents: string[]
  active: boolean
  selectionHint?: FileTarget
  onSelectionHintConsumed?: () => void
  onOpenFile: (target: FileTarget, placement?: OpenPlacement) => void
  onOpenFolder: (target: FolderTarget, placement?: OpenPlacement, selectionHint?: FileTarget) => void
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
    if (!selectionHint) return
    const hintedFile = folderSelectionTarget(target.root, target.path, selectionHint)
    onSelectionHintConsumed?.()
    if (!hintedFile) return
    setCurrentDir(target.path)
    setSelected(hintedFile)
    setViewMode(isMarkdownPath(hintedFile.path) && !hintedFile.line ? 'rendered' : 'source')
    setGitState(initialGitFileState())
    setBoardView(false)
  }, [onSelectionHintConsumed, selectionHint, target.path, target.root])

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
  const backlogFailure = backlog.error ? failureBanner('backlog', backlog.error) : null
  const parentFolder = parentFolderPath(currentDir)
  const absolutePath = rootJoinedAbsolutePath(target.root, currentDir)
  return <main className="folder-panel">
    <header className="folder-header">
      <div className="folder-title"><div className="path-name"><strong>{rootLabel(currentDir) || rootLabel(target.root)}</strong><PathCopyButton key={absolutePath} path={absolutePath} /><button type="button" className={`header-refresh${currentTree.isFetching || backlog.isFetching ? ' busy' : ''}`}
        title={currentTree.isFetching || backlog.isFetching ? 'Refreshing…' : 'Refresh'} aria-label="Refresh" onClick={() => { currentTree.refetch(); backlog.refetch() }} disabled={currentTree.isFetching || backlog.isFetching}>
        <svg viewBox="0 0 14 14" aria-hidden="true" focusable="false"><path d="M12 7a5 5 0 1 1-1.46-3.54" /><path d="M12.5 1.5v2.6h-2.6" /></svg>
      </button></div>{parentFolder !== null && <a className="path-parent-link"
        href={`/folder?${new URLSearchParams({ root: target.root, path: parentFolder })}`} title={`Open ${rootJoinedAbsolutePath(target.root, parentFolder)} · ${openInSideLabel(navigator.userAgent)}`}
        onClick={(event) => { event.preventDefault(); onOpenFolder({ root: target.root, path: parentFolder }, placementFromModifiers(event)) }}>{parentFolder || '.'}</a>}
        <span className="root-path" title={target.root}>{target.root}</span></div>
      <button type="button" onClick={() => setTreeHidden((value) => !value)}>{treeHidden ? 'Show tree' : 'Hide tree'}</button>
      {available && <div className="detail-toggle folder-view-toggle" aria-label="Folder view">
        <button type="button" className={!showBoard ? 'active' : ''} aria-pressed={!showBoard} onClick={() => setBoardView(false)}>Files</button>
        <button type="button" className={showBoard ? 'active' : ''} aria-pressed={showBoard} onClick={() => setBoardView(true)}>Board</button>
      </div>}
    </header>
    {backlogFailure && <Banner source={backlogFailure.source} detail={backlogFailure.detail} />}
    <div className={`folder-workspace${treeHidden ? ' tree-hidden' : ''}`}>
      {!treeHidden && <aside className="folder-tree panel-tree" role="tree" aria-label="Folder tree" onKeyDown={handleTreeKeyDown}><DirectoryTree root={target.root} path={target.path} depth={0} currentDir={currentDir} selectedFile={selected} onDirectory={chooseDirectory} onSelect={chooseFile} onOpenFile={onOpenFile} onOpenFolder={onOpenFolder} /></aside>}
      <section className="folder-detail">
        {showBoard && backlog.data && boardAvailable(backlog.data) ? <>
          <div className="backlog-facts"><span>Fetched {new Date(backlog.data.fetched_at).toLocaleString()}</span><span>{backlog.data.tasks.length} parsed tasks</span></div>
          <BacklogBoard backlog={backlog.data} onOpenFile={onOpenFile} />
        </> : selected ? <FilePanel target={selected} agents={agents} viewMode={viewMode} gitState={gitState} active={active} onViewMode={setViewMode} onGitState={setGitState} onOpenFile={onOpenFile} onOpenFolder={onOpenFolder} />
          : <PanelState as="div" className="folder-empty" title="Select a file" detail="A single click previews it here. Double-click or use Open tab to create a dock preview." />}
      </section>
    </div>
  </main>
}
