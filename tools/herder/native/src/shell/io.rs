//! The shell's background I/O: a REST read, or the outbox save and the posts it guards. Each takes the
//! client (and disk) and reports back as `Event`s; nothing here touches GPUI or view state.

use crate::api::client::Client;
use crate::harness;
use crate::local::{self, Disk};
use crate::store::sync::Step;
use crate::store::transcript::{self, Got, What};
use crate::store::{Event, Fetch, Write};

pub(super) fn run_fetch(client: &Client, fetch: Fetch) -> Event {
    match fetch {
        Fetch::Viewer => Event::Viewer(client.viewer().map(|v| v.viewer).map_err(|e| {
            eprintln!("viewer: {e}");
            e.status()
        })),
        Fetch::State { ns, since } => {
            let step = match client.state(ns.name(), since) {
                Ok(rows) => Step::Pulled(rows),
                Err(e) => {
                    eprintln!("state {}: {e}", ns.name());
                    Step::PullFailed(e.status())
                }
            };
            Event::Sync { ns, step }
        }
        Fetch::Transcript(read) => {
            let (started, agent) = (std::time::Instant::now(), read.agent.as_str());
            let result = match &read.what {
                What::Page(page) => client.entries(agent, page).map(|e| Got::Page(Box::new(e))),
                What::Detail => client.agent(agent).map(|d| Got::Detail(Box::new(d))),
                What::Resolve(query, _, scoped) => {
                    let scope = scoped.then_some(agent);
                    client.resolve(query, scope).map(Got::Resolved)
                }
            };
            if let (What::Page(page), Ok(Got::Page(e))) = (&read.what, &result) {
                let ms = started.elapsed().as_secs_f64() * 1e3;
                let n = e.entries.len();
                harness::metric(format!("read {agent} {page:?}: {n} entries in {ms:.1} ms"));
            }
            let result = result.map_err(|e| e.to_string());
            Event::Transcript(transcript::Step::Read(read, result))
        }
    }
}

/// Save the outbox, then post each write and report its answer. A failed save posts nothing: every
/// write comes back as a transport-style failure, which backs off and tries again (saving first again).
pub fn save_then_send(
    disk: &Disk,
    client: &Client,
    outbox: &[u8],
    seq: u64,
    sends: Vec<Write>,
    mut on: impl FnMut(Event),
) {
    let saved = disk.write(local::OUTBOX, outbox, seq);
    if let Err(e) = &saved {
        eprintln!("local: could not save outbox.json, not sending: {e}");
    }
    for Write::State { ns, rows } in sends {
        let step = match &saved {
            Err(_) => Step::PostFailed(None),
            Ok(()) => match client.post_state(ns.name(), &rows) {
                Ok(_) => Step::Posted,
                Err(e) => {
                    eprintln!("state {} post: {e}", ns.name());
                    Step::PostFailed(e.status())
                }
            },
        };
        on(Event::Sync { ns, step });
    }
}
