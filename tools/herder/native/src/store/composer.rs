//! The composer's domain (U4): one draft per agent (kept in `Prefs`, so it persists), whether an agent
//! can be written to, and each send's lifecycle. A send is decided here once, by the owner: nothing
//! (a `hello`, a retry timer, a reconnect) sends it again, because `POST …/message` has no idempotency
//! key and a message must never land twice.

use super::spaces::{self, Seen};
use super::{Attribution, Effect, Persist, Store, Write};
use crate::api::Refusal;

#[derive(Clone, Debug)]
pub enum Step {
    /// The box's text for `agent` changed.
    Edit { agent: String, text: String },
    /// `cmd-enter`: send `agent`'s draft. `file_back` (`cmd-shift-enter`): once it lands, mark the agent
    /// seen as it stood when sent and leave the zoom for the lens.
    Send { agent: String, file_back: bool },
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
        let open = self.transcript.open.as_ref().filter(|t| t.agent == agent);
        match open.and_then(|t| t.detail.as_ref()) {
            None => Err(ReadOnly::Pending),
            Some(d) if d.bus_status == "retired" => Err(ReadOnly::Retired),
            Some(_) => Ok(()),
        }
    }

    /// `agent`'s draft can go now: writable, not blank, and no send of it in flight.
    pub fn ready(&self, agent: &str) -> bool {
        let draft = self.prefs.drafts.get(agent);
        self.can_send(agent).is_ok()
            && !self.in_flight(agent)
            && draft.is_some_and(|d| !d.trim().is_empty())
    }

    pub fn in_flight(&self, agent: &str) -> bool {
        matches!(self.sends.get(agent), Some(Sending::InFlight { .. }))
    }

    /// A fleet frame: a pending file-back's block acknowledgement lapses once its agent is seen
    /// unblocked or gone, so blocking again at the same turn still needs the owner.
    pub(super) fn lapse_blocks(&mut self) {
        for (agent, sending) in &mut self.sends {
            if let Sending::InFlight {
                file_back: Some(then),
                ..
            } = sending
            {
                then.blocked &= spaces::looking(&self.fleet, agent).is_some_and(|now| now.blocked);
            }
        }
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
                    .then(|| spaces::looking(&self.fleet, &agent))
                    .flatten();
                let flight = Sending::InFlight {
                    text: text.clone(),
                    file_back,
                };
                self.sends.insert(agent.clone(), flight);
                out.push(Effect::Send(Write::Message { agent, text }));
            }
            Step::Sent { agent, result } => {
                let Some(Sending::InFlight { text, file_back }) = self.sends.remove(&agent) else {
                    return;
                };
                match result {
                    Ok(()) => {
                        if drafts.get(&agent) == Some(&text) {
                            drafts.remove(&agent);
                            out.push(Effect::Persist(Persist::Prefs));
                        }
                        // Filed back: only a send that landed lets the owner leave the agent, and only
                        // what they saw when sending is seen: a turn since still needs them.
                        if let Some(then) = file_back {
                            let seen = &mut self.prefs.seen;
                            if spaces::acknowledge(seen, &self.fleet, &agent, then) {
                                out.push(Effect::Persist(Persist::Prefs));
                            }
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
