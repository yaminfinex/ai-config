//! The composer's domain (U4): one draft per agent (kept in `Prefs`, so it persists), whether an agent
//! can be written to, and each send's lifecycle. A send is decided here once, by the owner: nothing
//! (a `hello`, a retry timer, a reconnect) sends it again, because `POST …/message` has no idempotency
//! key and a message must never land twice.

use super::spaces::Move;
use super::{Attribution, Effect, Persist, Store, Write};

#[derive(Clone, Debug)]
pub enum Step {
    /// The box's text for `agent` changed.
    Edit { agent: String, text: String },
    /// `cmd-enter`: send `agent`'s draft. `file_back`: the zoom left this space for the lens as it sent.
    Send {
        agent: String,
        file_back: Option<String>,
    },
    /// The server's answer, or why it was never asked.
    Sent {
        agent: String,
        result: Result<(), Failure>,
    },
}

/// Why a send did not land. Every one keeps the draft.
#[derive(Clone, Debug, PartialEq)]
pub enum Failure {
    /// 409: the server refused it (attribution, a sender collision, the substrate), with its reason.
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
    InFlight {
        text: String,
        file_back: Option<String>,
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
        if self.viewer == Attribution::Refused {
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
                    }
                    Err(failure) => {
                        self.sends.insert(agent, Sending::Failed(failure));
                        // Filed back before the answer: the space needs the owner again, to see why.
                        if let Some(space) = file_back {
                            self.lens_move(Move::Unread(space), out);
                        }
                    }
                }
            }
        }
    }
}

impl ReadOnly {
    /// What the composer says under the box.
    pub fn say(self) -> &'static str {
        match self {
            ReadOnly::Refused => "read-only: the server refused this Mac's attribution",
            ReadOnly::OffBoard => "read-only: not on the board",
            ReadOnly::Pending => "waiting for the agent's details…",
            ReadOnly::Retired => "retired · read-only",
        }
    }
}

impl Failure {
    /// What the composer says under the box.
    pub fn say(&self) -> String {
        match self {
            Failure::Refused(why) => format!("refused: {why}"),
            Failure::Unreachable(why) => format!("unreachable, not sent ({why}) · ⌘⏎ retry"),
            Failure::UnknownAgent => "the server knows no such agent".into(),
            Failure::Rejected(status, why) => format!("rejected ({status}): {why}"),
            Failure::NoAnswer(why) => {
                format!(
                    "no answer ({why}): it may have been sent; check the transcript before retrying"
                )
            }
            Failure::NotSaved(why) => format!("not sent: the draft could not be saved ({why})"),
        }
    }
}
