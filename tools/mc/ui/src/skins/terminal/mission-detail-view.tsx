import type { MissionDetailViewProps } from "@/skins/contract";
import type { ThreadRowVM } from "@/view-models/mission-detail";

export function TerminalMissionDetailView({
  vm,
  loading,
  failure,
  stale,
  activeThreadId,
  onToggleThread,
}: MissionDetailViewProps) {
  if (failure !== null) {
    return (
      <p data-testid="load-failure" className="p-4 font-fact text-sm text-warn">
        ▲ {failure}
      </p>
    );
  }
  if (loading || vm === null) {
    return <p className="p-4 text-quiet">… loading</p>;
  }
  const active = vm.threads.find((thread) => thread.id === activeThreadId) ?? null;
  return (
    <div className="mx-auto max-w-3xl space-y-4 p-4 font-fact text-sm">
      <header>
        <h1 className="uppercase tracking-widest">== {vm.title} ==</h1>
        {(vm.facts !== null || vm.taskSummary !== null) && (
          <p className="text-quiet">
            {vm.facts !== null && <span data-testid="mission-facts">{vm.facts}</span>}
            {vm.facts !== null && vm.taskSummary !== null && " "}
            {vm.taskSummary !== null && <span data-testid="task-summary">[{vm.taskSummary}]</span>}
          </p>
        )}
        {stale !== null && (
          <p data-testid="stale-warning" className="text-warn">
            ▲ {stale}
          </p>
        )}
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
              // The message body is human speech: font-speech even though
              // this skin values both grains with the same face — the grain
              // is semantic, the face is the skin's choice (two-grain law).
              <p data-testid="active-thread-text" className="font-speech">
                <span className="font-fact text-quiet">{active.preview.from}&gt; </span>
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
        {vm.threadsEmpty && (
          <p data-testid="threads-empty" className="text-quiet">
            (no threads)
          </p>
        )}
        {vm.threads.length > 0 && (
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
        )}
      </section>

      <section aria-label="crew" className="space-y-1">
        <h2 className="uppercase tracking-widest text-quiet">crew</h2>
        {vm.rosterWarning !== null && (
          <p data-testid="roster-warning" className="text-warn">
            ▲ {vm.rosterWarning}
          </p>
        )}
        {vm.crewEmpty && (
          <p data-testid="crew-empty" className="text-quiet">
            (no agents)
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
