import type { MissionDetailViewProps } from "@/skins/contract";
import type { ThreadRowVM } from "@/view-models/mission-detail";

export function TerminalMissionDetailView({
  vm,
  loading,
  activeThreadId,
  onToggleThread,
}: MissionDetailViewProps) {
  if (loading || vm === null) {
    return <p className="p-4 text-quiet">… loading</p>;
  }
  const active = vm.threads.find((thread) => thread.id === activeThreadId) ?? null;
  return (
    <div className="mx-auto max-w-3xl space-y-4 p-4 font-fact text-sm">
      <header>
        <h1 className="uppercase tracking-widest">== {vm.title} ==</h1>
        {vm.warnings.map((warning) => (
          <p key={warning} data-testid="mission-warning" className="text-warn">
            ▲ {warning}
          </p>
        ))}
      </header>

      {/* terminal's declared behavioural rendering: the active thread renders
          in a dedicated PANEL above the list, never inside a row. */}
      <div data-testid="active-thread-panel" className="min-h-16 border border-border p-2">
        {active ? (
          <div data-testid="active-thread" className="space-y-1">
            <p className="text-quiet">
              ┌ {active.title} · {active.facts}
            </p>
            {active.preview ? (
              <p data-testid="active-thread-text">
                <span className="text-quiet">{active.preview.from}&gt; </span>
                {active.preview.text}
              </p>
            ) : (
              <p data-testid="active-thread-text" className="text-quiet">
                no messages
              </p>
            )}
          </div>
        ) : (
          <p className="text-quiet">no thread engaged</p>
        )}
      </div>

      <section aria-label="threads" className="space-y-1">
        <h2 className="uppercase tracking-widest text-quiet">threads</h2>
        <ul className="divide-y divide-border border border-border">
          {vm.threads.map((thread) => (
            <ThreadRow
              key={thread.id}
              thread={thread}
              active={thread.id === activeThreadId}
              onToggle={() => onToggleThread(thread.id)}
            />
          ))}
        </ul>
      </section>

      <section aria-label="crew" className="space-y-1">
        <h2 className="uppercase tracking-widest text-quiet">crew</h2>
        {vm.rosterWarning !== null && (
          <p data-testid="roster-warning" className="text-warn">
            ▲ {vm.rosterWarning}
          </p>
        )}
        <ul>
          {vm.agents.map((agent) => (
            <li key={agent.name} data-testid="agent-row">
              {agent.name} <span className="text-quiet">[{agent.facts}]</span>
            </li>
          ))}
        </ul>
      </section>
    </div>
  );
}

function ThreadRow({
  thread,
  active,
  onToggle,
}: {
  thread: ThreadRowVM;
  active: boolean;
  onToggle: () => void;
}) {
  return (
    <li data-thread-id={thread.id}>
      <button
        type="button"
        data-testid="thread-row"
        className="grid w-full grid-cols-[2ch_5rem_1fr_auto] gap-1 p-1 text-left hover:bg-accent"
        onClick={onToggle}
      >
        <span className="text-primary">{active ? "▶" : " "}</span>
        <span className={thread.status === "open" ? "text-foreground" : "text-quiet"}>
          {thread.expects}
        </span>
        <span>{thread.title}</span>
        <span className="text-quiet">{thread.facts}</span>
      </button>
    </li>
  );
}
