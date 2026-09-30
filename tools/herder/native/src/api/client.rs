//! Blocking HTTP against herder serve. Called from background threads only.
//!
//! One function per endpoint, each returning a `types` model or an `Error`. Sends are plain POSTs with
//! exactly the documented fields (the server rejects unknown ones). Retries are the caller's business:
//! `POST …/message` has no idempotency key, so a blind retry can send twice.

use crate::api::types::{Board, Refusal};
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

fn agent() -> ureq::Agent {
    ureq::AgentBuilder::new()
        .timeout_connect(Duration::from_secs(5))
        .timeout_read(Duration::from_secs(60))
        .build()
}

fn get<T: serde::de::DeserializeOwned>(path: &str) -> Result<T, Error> {
    Ok(agent()
        .get(&format!("{}{path}", base_url()))
        .call()?
        .into_json()?)
}

/// `GET /api/fleet`.
pub fn fleet() -> Result<Board, Error> {
    get("/api/fleet")
}
