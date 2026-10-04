//! The composer's domain (U4): one draft per agent (kept in `Prefs`, so it persists), whether an agent
//! can be written to, and each send's lifecycle. A send is decided here once, by the owner: nothing
//! (a `hello`, a retry timer, a reconnect) sends it again, because `POST …/message` has no idempotency
//! key and a message must never land twice.

use super::attention::{self, Seen};
use super::markers::Mark;
use super::{Attribution, Effect, Persist, Store};
use crate::api::Refusal;

#[derive(Clone, Debug)]
pub enum Step {
    /// The box's text for `agent` changed.
    Edit { agent: String, text: String },
    /// `cmd-enter`: send `agent`'s draft. `file_back` (`cmd-shift-enter`): once it lands, mark the agent
    /// seen as it stood when sent and leave the zoom for the lens.
    Send { agent: String, file_back: bool },
    /// `cmd-enter` in the capture popover (F7): send `text` (a note, as web's `noteTransferText`) on its
    /// own, the draft untouched (web's quick send). It is saved (`Prefs::quick`) before it goes, as a
    /// draft is; if it fails, or the app stops before the answer, the text is added to the draft.
    Quick { agent: String, text: String },
    /// The server's answer, or why it was never asked.
    Sent {
        agent: String,
        result: Result<(), Failure>,
    },
}

/// Why a send did not land. Every one keeps the draft.
#[derive(Clone, Debug, PartialEq)]
pub enum Failure {
    /// 409 `attribution required` or `sender refused`: no write from this Mac will land, so every box
    /// goes read-only with the server's reason (`Attribution::Refused`).
    Unattributed(Refusal),
    /// Any other 409 (a retired agent, the substrate), with its reason.
    Refused(String),
    /// 502: the server could not reach the bus; nothing was sent.
    Unreachable(String),
    /// 404: the server knows no such agent.
    UnknownAgent,
    /// Any other status (400: a bad body).
    Rejected(u16, String),
    /// No answer came back: it may or may not have been delivered.
    NoAnswer(String),
    /// The draft could not be saved first, so nothing was sent.
    NotSaved(String),
}

/// A send in flight, or the last one's failure, per agent.
#[derive(Clone, Debug, PartialEq)]
pub enum Sending {
    /// `file_back`: the agent as the owner left it, which the landing acknowledges.
    InFlight {
        text: String,
        file_back: Option<Seen>,
        /// A quick send (`Step::Quick`): its text is not the draft.
        quick: bool,
    },
    Failed(Failure),
}

/// Why an agent's box is read-only.
#[derive(Clone, Copy, Debug, PartialEq, Eq)]
pub enum ReadOnly {
    /// The server refused this Mac's attribution (`GET /api/viewer` 409).
    Refused,
    /// Not on the board: gone, or a preview of an agent that left.
    OffBoard,
    /// Its detail has not arrived, so whether it is retired is not known yet.
    Pending,
    Retired,
}

impl Store {
    /// Whether the owner can write to `agent`. `Unknown` attribution may send: the server decides.
    pub fn can_send(&self, agent: &str) -> Result<(), ReadOnly> {
        if matches!(self.viewer, Attribution::Refused(_)) {
            return Err(ReadOnly::Refused);
        }
        if !self.fleet.agents.contains_key(agent) {
            return Err(ReadOnly::OffBoard);
        }
        let open = self.transcript.open.get(agent);
        match open.filter(|t| t.detail.is_some()) {
            None => Err(ReadOnly::Pending),
            Some(t) if t.retired() => Err(ReadOnly::Retired),
            Some(_) => Ok(()),
        }
    }

    /// `agent`'s draft can go now: writable, not blank, no send of it in flight and no note transfer
    /// (U5) waiting on a save that may still change the draft.
    pub fn ready(&self, agent: &str) -> bool {
        let draft = self.prefs.drafts.get(agent);
        self.can_send(agent).is_ok()
            && !self.busy(agent)
            && draft.is_some_and(|d| !d.trim().is_empty())
    }

    pub fn in_flight(&self, agent: &str) -> bool {
        matches!(self.sends.get(agent), Some(Sending::InFlight { .. }))
    }

    /// A send of `agent`'s draft or a note transfer of it is in flight: one excludes the other (U5).
    pub fn busy(&self, agent: &str) -> bool {
        self.in_flight(agent) || self.transfers.contains_key(agent)
    }

    pub(super) fn compose(&mut self, step: Step, out: &mut Vec<Effect>) {
        // The box is disabled while a send is in flight; an edit then would race its answer.
        if let Step::Edit { agent, .. } = &step
            && self.in_flight(agent)
        {
            return;
        }
        let drafts = &mut self.prefs.drafts;
        match step {
            Step::Edit { agent, text } => {
                self.sends.remove(&agent);
                let changed = match text.is_empty() {
                    true => drafts.remove(&agent).is_some(),
                    false => drafts.insert(agent, text.clone()) != Some(text),
                };
                if changed {
                    out.push(Effect::Persist(Persist::Prefs));
                }
            }
            Step::Send { agent, file_back } => {
                if !self.ready(&agent) {
                    return;
                }
                let text = self.prefs.drafts[&agent].clone();
                let file_back = file_back
                    .then(|| attention::looking(&self.fleet, &agent))
                    .flatten();
                let flight = Sending::InFlight {
                    text: text.clone(),
                    file_back,
                    quick: false,
                };
                self.sends.insert(agent.clone(), flight);
                out.push(Effect::Message { agent, text });
            }
            Step::Quick { agent, text } => {
                if self.can_send(&agent).is_err() || self.busy(&agent) || text.trim().is_empty() {
                    return;
                }
                let flight = Sending::InFlight {
                    text: text.clone(),
                    file_back: None,
                    quick: true,
                };
                self.sends.insert(agent.clone(), flight);
                // In the prefs `Effect::Message` saves before it posts (a failed save posts nothing).
                self.prefs.quick.insert(agent.clone(), text.clone());
                out.push(Effect::Message { agent, text });
            }
            Step::Sent { agent, result } => {
                let Some(Sending::InFlight {
                    text,
                    file_back,
                    quick,
                }) = self.sends.remove(&agent)
                else {
                    return;
                };
                // A quick send is answered: no longer pending. One that did not land is kept in the
                // draft, as web appends it to the prompt.
                if quick {
                    self.prefs.quick.remove(&agent);
                    if result.is_err() {
                        keep(&mut self.prefs.drafts, &agent, &text);
                    }
                    out.push(Effect::Persist(Persist::Prefs));
                }
                let drafts = &mut self.prefs.drafts;
                match result {
                    Ok(()) => {
                        if !quick && drafts.get(&agent) == Some(&text) {
                            drafts.remove(&agent);
                            out.push(Effect::Persist(Persist::Prefs));
                        }
                        // Filed back: only a send that landed lets the owner leave the agent, and only
                        // what they saw when sending is seen: a turn since still needs them.
                        if let Some(then) = file_back {
                            let blocks = &mut self.prefs.blocks;
                            if attention::acknowledge(blocks, &self.fleet, &agent, then) {
                                out.push(Effect::Persist(Persist::Prefs));
                            }
                            let read = Mark::Read(vec![agent.clone()], Some(then.turn_end));
                            self.mark(read, out);
                            out.push(Effect::FiledBack { agent });
                        }
                    }
                    Err(Failure::Unattributed(why)) => {
                        self.viewer = Attribution::Refused(Some(why))
                    }
                    Err(failure) => {
                        self.sends.insert(agent, Sending::Failed(failure));
                    }
                }
            }
        }
    }
}

impl Store {
    /// At boot, quick sends the app stopped before hearing back about: each into its agent's draft,
    /// not sent again (it may have landed).
    pub(super) fn recover(&mut self, out: &mut Vec<Effect>) {
        let pending = std::mem::take(&mut self.prefs.quick);
        for (agent, text) in &pending {
            keep(&mut self.prefs.drafts, agent, text);
        }
        if !pending.is_empty() {
            out.push(Effect::Persist(Persist::Prefs));
        }
    }
}

/// `text` after `agent`'s draft, a blank line between.
fn keep(drafts: &mut std::collections::BTreeMap<String, String>, agent: &str, text: &str) {
    let draft = drafts.entry(agent.to_string()).or_default();
    *draft = match draft.is_empty() {
        true => text.to_string(),
        false => format!("{draft}\n\n{text}"),
    };
}
