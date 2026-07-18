import { Badge } from "@/components/ui/badge";
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import type { MissionListViewProps } from "@/skins/contract";

export function MinimalMissionListView({
  vm,
  loading,
  failure,
  onOpenMission,
}: MissionListViewProps) {
  if (failure !== null) {
    return (
      <p data-testid="load-failure" className="p-6 font-fact text-sm text-warn">
        ▲ {failure}
      </p>
    );
  }
  if (loading) {
    return <p className="p-6 text-muted-foreground">loading…</p>;
  }
  return (
    <div className="mx-auto max-w-2xl space-y-4 p-6">
      <h1 className="text-lg font-semibold">missions</h1>
      {vm.warning !== null && (
        <p data-testid="list-warning" className="font-fact text-sm text-warn">
          ▲ {vm.warning}
        </p>
      )}
      {vm.rows.length === 0 && (
        <p data-testid="missions-empty" className="font-fact text-sm text-quiet">
          no missions
        </p>
      )}
      <ul className="space-y-3">
        {vm.rows.map((row) => (
          <li key={row.slug}>
            <button
              type="button"
              data-testid="mission-row"
              data-slug={row.slug}
              className="w-full text-left"
              onClick={() => onOpenMission(row.slug)}
            >
              <Card className="cursor-pointer transition-colors hover:bg-accent/40">
                <CardHeader>
                  <CardTitle className="flex items-center gap-2">
                    {row.title}
                    {!row.healthy && (
                      <Badge variant="outline" className="border-warn text-warn">
                        ▲
                      </Badge>
                    )}
                  </CardTitle>
                </CardHeader>
                <CardContent className="space-y-1 font-fact text-sm text-muted-foreground">
                  {row.owner !== null && <div>owner {row.owner}</div>}
                  {row.taskSummary !== null && (
                    <div data-testid="task-summary">{row.taskSummary}</div>
                  )}
                  {row.warnings.map((warning) => (
                    <div key={warning} className="text-warn">
                      ▲ {warning}
                    </div>
                  ))}
                </CardContent>
              </Card>
            </button>
          </li>
        ))}
      </ul>
    </div>
  );
}
