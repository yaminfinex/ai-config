import type { FleetView } from '../layout/shellPreferences'

const views: { view: FleetView, label: string, title: string }[] = [
  { view: 'supervision', label: 'tree', title: 'Supervision tree: who manages whom' },
  { view: 'placement', label: 'placement', title: 'Placement: workspaces, tabs and panes' },
]

// FleetViewToggle sits in the Fleet rail heading and switches the sidebar
// between the supervision tree and the placement tree. The choice persists
// with the shell preferences; expanded state is shared, never reset.
export function FleetViewToggle({ view, onView }: { view: FleetView, onView: (view: FleetView) => void }) {
  return <span className="fleet-view-toggle" role="group" aria-label="Fleet view">
    {views.map((option) => <button key={option.view} type="button" className={`fleet-view-option${option.view === view ? ' active' : ''}`}
      aria-pressed={option.view === view} title={option.title} onClick={() => onView(option.view)}>{option.label}</button>)}
  </span>
}
