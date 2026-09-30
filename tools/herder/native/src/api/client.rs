//! Blocking HTTP against herder serve. Called from background threads only.
//!
//! One method per endpoint, each returning a `types` model or an `Error`. Sends are plain POSTs with
//! exactly the documented fields (the server rejects unknown ones). Retries are the caller's business:
//! `POST …/message` has no idempotency key, so a blind retry can send twice.

use crate::api::types::{
    Accepted, AgentDetail, Board, Entries, Refusal, Resolved, StateRow, StateRows, Viewer,
};
use serde::Serialize;
use std::time::Duration;

const DEFAULT_URL: &str = "http://yamen-superset-f4-med-syd-1:4400";

/// The server, from `HERDER_URL` or the default tailnet address. Never loopback (settled decision 6).
pub fn base_url() -> String {
    std::env::var("HERDER_URL").unwrap_or_else(|_| DEFAULT_URL.into())
}

/// Transport failure, or a refusal the server explained.
#[derive(Debug)]
pub enum Error {
    Transport(String),
    Refused { status: u16, refusal: Refusal },
}

impl Error {
    /// The HTTP status of a refusal; `None` for a transport failure.
    pub fn status(&self) -> Option<u16> {
        match self {
            Error::Transport(_) => None,
            Error::Refused { status, .. } => Some(*status),
        }
    }
}

impl std::fmt::Display for Error {
    fn fmt(&self, f: &mut std::fmt::Formatter<'_>) -> std::fmt::Result {
        match self {
            Error::Transport(e) => f.write_str(e),
            Error::Refused { status, refusal } => {
                write!(f, "{status} {}: {}", refusal.error, refusal.detail)
            }
        }
    }
}

impl From<ureq::Error> for Error {
    fn from(e: ureq::Error) -> Self {
        match e {
            ureq::Error::Status(status, r) => {
                let refusal = r.into_json().unwrap_or_default();
                Error::Refused { status, refusal }
            }
            e => Error::Transport(e.to_string()),
        }
    }
}

impl From<std::io::Error> for Error {
    fn from(e: std::io::Error) -> Self {
        Error::Transport(e.to_string())
    }
}

/// Which window of a transcript to read. `session` pins the offsets to one session (`sessionId`).
#[derive(Clone, Debug, PartialEq, Eq)]
pub enum Page {
    Tail {
        limit: u32,
    },
    From {
        offset: u64,
        session: String,
        limit: u32,
    },
    Before {
        offset: u64,
        session: String,
        limit: u32,
    },
}

/// One server. Cheap to clone; clones share the connection pool.
#[derive(Clone)]
pub struct Client {
    base: String,
    http: ureq::Agent,
}

impl Client {
    pub fn new(base: impl Into<String>) -> Self {
        let http = ureq::AgentBuilder::new()
            .timeout_connect(Duration::from_secs(5))
            .timeout_read(Duration::from_secs(60))
            .build();
        Client {
            base: base.into(),
            http,
        }
    }

    pub fn base(&self) -> &str {
        &self.base
    }

    fn get(&self, path: &str) -> ureq::Request {
        self.http.get(&format!("{}{path}", self.base))
    }

    fn post<T: serde::de::DeserializeOwned>(
        &self,
        path: &str,
        body: impl Serialize,
    ) -> Result<T, Error> {
        Ok(self
            .http
            .post(&format!("{}{path}", self.base))
            .send_json(body)?
            .into_json()?)
    }

    /// `GET /api/viewer`: who this Mac is attributed as. 409 means writes will be refused.
    pub fn viewer(&self) -> Result<Viewer, Error> {
        Ok(self.get("/api/viewer").call()?.into_json()?)
    }

    /// `GET /api/fleet`.
    pub fn fleet(&self) -> Result<Board, Error> {
        Ok(self.get("/api/fleet").call()?.into_json()?)
    }

    /// `GET /api/agents/{name}`.
    pub fn agent(&self, name: &str) -> Result<AgentDetail, Error> {
        Ok(self
            .get(&format!("/api/agents/{name}"))
            .call()?
            .into_json()?)
    }

    /// `GET /api/agents/{name}/entries` for one window.
    pub fn entries(&self, name: &str, page: &Page) -> Result<Entries, Error> {
        let req = self.get(&format!("/api/agents/{name}/entries"));
        let req = match page {
            Page::Tail { limit } => req.query("limit", &limit.to_string()),
            Page::From {
                offset,
                session,
                limit,
            } => req
                .query("from", &offset.to_string())
                .query("sessionId", session)
                .query("limit", &limit.to_string()),
            Page::Before {
                offset,
                session,
                limit,
            } => req
                .query("before", &offset.to_string())
                .query("sessionId", session)
                .query("limit", &limit.to_string()),
        };
        Ok(req.call()?.into_json()?)
    }

    /// `GET /api/resolve?q=&agent=`: where a path mentioned in `agent`'s transcript lives.
    pub fn resolve(&self, q: &str, agent: &str) -> Result<Resolved, Error> {
        let req = self.get("/api/resolve").query("q", q).query("agent", agent);
        Ok(req.call()?.into_json()?)
    }

    /// `GET /api/state/{ns}?since=`. A namespace nobody has written yet is empty, not an error.
    pub fn state(&self, ns: &str, since: u64) -> Result<StateRows, Error> {
        let got = self
            .get(&format!("/api/state/{ns}"))
            .query("since", &since.to_string())
            .call();
        match got {
            Ok(r) => Ok(r.into_json()?),
            Err(ureq::Error::Status(404, r)) => {
                let refusal: Refusal = r.into_json().unwrap_or_default();
                if refusal.error == "state namespace not found" {
                    Ok(StateRows::default())
                } else {
                    Err(Error::Refused {
                        status: 404,
                        refusal,
                    })
                }
            }
            Err(e) => Err(e.into()),
        }
    }

    /// `POST /api/state/{ns}` with `{rows}`.
    pub fn post_state(&self, ns: &str, rows: &[StateRow]) -> Result<Accepted, Error> {
        #[derive(Serialize)]
        struct Body<'a> {
            rows: &'a [StateRow],
        }
        self.post(&format!("/api/state/{ns}"), Body { rows })
    }

    /// `POST /api/agents/{name}/message` with `{text}`. Never retried blindly (see the module doc).
    pub fn send_message(&self, name: &str, text: &str) -> Result<(), Error> {
        #[derive(Serialize)]
        struct Body<'a> {
            text: &'a str,
        }
        let _: serde_json::Value =
            self.post(&format!("/api/agents/{name}/message"), Body { text })?;
        Ok(())
    }
}
