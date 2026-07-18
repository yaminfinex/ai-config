import { Badge } from "@/components/ui/badge";
import { Separator } from "@/components/ui/separator";
import type { MissionDetailViewProps } from "@/skins/contract";
import type { ThreadRowVM } from "@/view-models/mission-detail";

export function MinimalMissionDetailView({
  vm,
  loading,
  activeThreadId,
  onToggleThread,
}: MissionDetailViewProps) {
  if (loading || vm === null) {
    return <p className="p-6 text-muted-foreground">loading…</p>;
  }
  return (
    <div className="mx-auto max-w-2xl space-y-6 p-6">
      <header className="space-y-1">
        <h1 className="text-lg font-semibold">{vm.title}</h1>
        {vm.warnings.map((warning) => (
          <p key={warning} data-testid="mission-warning" className="font-fact text-sm text-warn">
            ▲ {warning}
          </p>
        ))}
      </header>

      <section aria-label="crew" className="space-y-2">
        <h2 className="text-sm font-medium text-muted-foreground">crew</h2>
        {vm.rosterWarning !== null && (
          <p data-testid="roster-warning" className="font-fact text-sm text-warn">
            ▲ {vm.rosterWarning}
          </p>
        )}
        <ul className="space-y-1">
          {vm.agents.map((agent) => (
            <li key={agent.name} data-testid="agent-row" className="font-fact text-sm">
              {agent.name} <span className="text-muted-foreground">{agent.facts}</span>
            </li>
          ))}
        </ul>
      </section>

      <Separator />

      <section aria-label="threads" className="space-y-2">
        <h2 className="text-sm font-medium text-muted-foreground">threads</h2>
        <ul className="space-y-2">
          {vm.threads.map((thread) => (
            <li key={thread.id} data-thread-id={thread.id} className="rounded-lg border">
              <button
                type="button"
                data-testid="thread-row"
                className="flex w-full items-center gap-2 p-3 text-left hover:bg-accent/40"
                onClick={() => onToggleThread(thread.id)}
              >
                <Badge variant={thread.status === "open" ? "default" : "secondary"}>
                  {thread.expects}
                </Badge>
                <span className="flex-1">{thread.title}</span>
                <span className="font-fact text-xs text-muted-foreground">{thread.facts}</span>
              </button>
              {thread.id === activeThreadId && <ActiveThread thread={thread} />}
            </li>
          ))}
        </ul>
      </section>
    </div>
  );
}

// minimal's declared behavioural rendering: the active thread expands
// IN-ROW, beneath the row that opened it.
function ActiveThread({ thread }: { thread: ThreadRowVM }) {
  return (
    <div data-testid="active-thread" className="space-y-1 border-t bg-card p-3">
      {thread.preview ? (
        <>
          <p className="font-fact text-xs text-muted-foreground">{thread.preview.from}</p>
          <p data-testid="active-thread-text" className="font-speech text-sm">
            {thread.preview.text}
          </p>
        </>
      ) : (
        <p data-testid="active-thread-text" className="font-fact text-sm text-muted-foreground">
          no messages
        </p>
      )}
    </div>
  );
}
