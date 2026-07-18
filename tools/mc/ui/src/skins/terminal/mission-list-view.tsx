import type { MissionListViewProps } from "@/skins/contract";

export function TerminalMissionListView({
  vm,
  loading,
  failure,
  onOpenMission,
}: MissionListViewProps) {
  if (failure !== null) {
    return (
      <p data-testid="load-failure" className="p-4 font-fact text-sm text-warn">
        ▲ {failure}
      </p>
    );
  }
  if (loading) {
    return <p className="p-4 text-quiet">… loading</p>;
  }
  return (
    <div className="mx-auto max-w-3xl space-y-2 p-4">
      <h1 className="font-fact text-sm uppercase tracking-widest text-quiet">== missions ==</h1>
      {vm.warning !== null && (
        <p data-testid="list-warning" className="font-fact text-sm text-warn">
          ▲ {vm.warning}
        </p>
      )}
      {vm.rows.length === 0 && (
        <p data-testid="missions-empty" className="font-fact text-sm text-quiet">
          (no missions)
        </p>
      )}
      {vm.rows.length > 0 && (
        <ul className="divide-y divide-border border border-border">
          {vm.rows.map((row) => (
            <li key={row.slug}>
              <button
                type="button"
                data-testid="mission-row"
                data-slug={row.slug}
                className="grid w-full grid-cols-[1fr_auto] gap-2 p-2 text-left font-fact text-sm hover:bg-accent"
                onClick={() => onOpenMission(row.slug)}
              >
                <span>
                  {row.healthy ? "  " : "▲ "}
                  {row.title}
                  {row.owner !== null && <span className="text-quiet"> @{row.owner}</span>}
                </span>
                {row.taskSummary !== null && (
                  <span data-testid="task-summary" className="text-quiet">
                    [{row.taskSummary}]
                  </span>
                )}
              </button>
              {row.warnings.length > 0 && (
                <div className="p-2 pt-0 font-fact text-xs text-warn">
                  {row.warnings.map((warning) => (
                    <div key={warning}>▲ {warning}</div>
                  ))}
                </div>
              )}
            </li>
          ))}
        </ul>
      )}
    </div>
  );
}
