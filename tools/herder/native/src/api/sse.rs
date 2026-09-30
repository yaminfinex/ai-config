//! The `/api/events` stream: `event: <type>\ndata: <json>\n\n` frames with no `id:` line, so nothing
//! can be resumed; after a reconnect the client re-reads from REST.
//!
//! The connection is a plain `TcpStream` speaking HTTP/1.0 (the server is plain HTTP, and 1.0 keeps the
//! body unchunked). The read blocks on its own thread, so an idle stream costs no CPU, and `Stop`
//! (a clone of the socket) shuts it down from any thread so a subscription change never waits for the
//! read timeout. `Reader` is that thread: it reopens with backoff 500 ms doubling to 10 s, and the 45 s
//! read timeout is the watchdog (the server pings every 15 s).

use crate::api::types::Wire;
use serde::Deserialize;
use std::io::{self, BufRead, BufReader, Write};
use std::net::{Shutdown, TcpStream, ToSocketAddrs};
use std::sync::atomic::{AtomicBool, Ordering};
use std::sync::{Arc, Mutex};
use std::time::Duration;

const BACKOFF: Duration = Duration::from_millis(500);
const BACKOFF_MAX: Duration = Duration::from_secs(10);

/// One server-sent event. `event` is `message` when the frame had no `event:` line.
#[derive(Clone, Debug, Default, PartialEq, Eq)]
pub struct Frame {
    pub event: String,
    pub data: String,
}

impl Wire {
    /// Decode a frame on the stream's thread, so the board's JSON never costs the foreground anything.
    pub fn decode(frame: &Frame) -> Wire {
        #[derive(Deserialize)]
        #[serde(rename_all = "camelCase")]
        struct Hello {
            build_identity: String,
        }
        #[derive(Deserialize)]
        struct StateChanged {
            namespace: String,
            rev: u64,
        }
        #[derive(Deserialize)]
        struct Rewindow {
            agent: String,
        }
        let data = frame.data.as_str();
        let decoded = match frame.event.as_str() {
            "hello" => serde_json::from_str(data).map(|h: Hello| Wire::Hello {
                build_identity: h.build_identity,
            }),
            "fleet" => serde_json::from_str(data).map(Wire::Fleet),
            "state-changed" => {
                serde_json::from_str(data).map(|s: StateChanged| Wire::StateChanged {
                    namespace: s.namespace,
                    rev: s.rev,
                })
            }
            "rewindow" => {
                serde_json::from_str(data).map(|r: Rewindow| Wire::Rewindow { agent: r.agent })
            }
            "ping" => Ok(Wire::Ping),
            event => match event.strip_prefix("entry:") {
                Some(agent) => serde_json::from_str(data).map(|entry| Wire::Entry {
                    agent: agent.to_string(),
                    entry,
                }),
                None => return Wire::Other(event.to_string()),
            },
        };
        decoded.unwrap_or_else(|_| Wire::Other(frame.event.clone()))
    }
}

/// Shuts the stream's socket down; the blocked reader returns at once. Cheap to clone and send.
#[derive(Clone)]
pub struct Stop(Arc<TcpStream>);

impl Stop {
    pub fn stop(&self) {
        let _ = self.0.shutdown(Shutdown::Both);
    }
}

pub struct Connection {
    pub reader: BufReader<TcpStream>,
    pub stop: Stop,
}

/// Open `GET {base_url}/api/events?{query}`. Fails on anything but a 200.
pub fn open(base_url: &str, query: &str) -> io::Result<Connection> {
    let bad = |what: &str| io::Error::new(io::ErrorKind::InvalidInput, what.to_string());
    let rest = base_url
        .strip_prefix("http://")
        .ok_or_else(|| bad("server url must be http://"))?;
    let host_port = rest.split('/').next().unwrap_or(rest);
    let (host, port) = match host_port.rsplit_once(':') {
        Some((h, p)) => (h, p.parse::<u16>().map_err(|_| bad("bad port"))?),
        None => (host_port, 80),
    };
    let addr = (host, port)
        .to_socket_addrs()?
        .next()
        .ok_or_else(|| bad("host did not resolve"))?;
    let stream = TcpStream::connect_timeout(&addr, Duration::from_secs(5))?;
    stream.set_read_timeout(Some(Duration::from_secs(45)))?;
    let stop = Stop(Arc::new(stream.try_clone()?));
    let mut writer = stream.try_clone()?;
    write!(
        writer,
        "GET /api/events?{query} HTTP/1.0\r\nHost: {host_port}\r\nAccept: text/event-stream\r\n\r\n"
    )?;
    let mut reader = BufReader::with_capacity(1 << 16, stream);
    let mut line = String::new();
    reader.read_line(&mut line)?;
    if !line.contains(" 200 ") {
        return Err(io::Error::other(format!("events: {}", line.trim())));
    }
    while reader.read_line(&mut line)? > 0 && !line.trim_end_matches(['\r', '\n']).is_empty() {
        line.clear();
    }
    Ok(Connection { reader, stop })
}

/// Reads frames until the reader ends or fails. `data:` lines may be many kilobytes (the whole board).
pub fn read_frames<R: BufRead>(mut reader: R, mut on: impl FnMut(Frame)) -> io::Result<()> {
    let mut frame = Frame::default();
    let mut line = String::new();
    loop {
        line.clear();
        if reader.read_line(&mut line)? == 0 {
            return Ok(());
        }
        let l = line.trim_end_matches(['\r', '\n']);
        if l.is_empty() {
            if !frame.event.is_empty() || !frame.data.is_empty() {
                if frame.event.is_empty() {
                    frame.event = "message".into();
                }
                on(std::mem::take(&mut frame));
            }
        } else if let Some(v) = l.strip_prefix("event:") {
            frame.event = v.trim().to_string();
        } else if let Some(v) = l.strip_prefix("data:") {
            if !frame.data.is_empty() {
                frame.data.push('\n');
            }
            frame.data.push_str(v.strip_prefix(' ').unwrap_or(v));
        }
        // `: ping` comments and `id:`/`retry:` lines are ignored.
    }
}

/// The stream's thread: open, read frames until the connection ends, report the drop, back off, reopen.
/// `close` ends it at once, and nothing is reported after that.
pub struct Reader {
    stop: Arc<Mutex<Option<Stop>>>,
    closed: Arc<AtomicBool>,
}

impl Reader {
    /// `on(Some(wire))` for each frame, `on(None)` each time a connection ends or fails to open.
    pub fn spawn(
        base: String,
        query: String,
        mut on: impl FnMut(Option<Wire>) + Send + 'static,
    ) -> Reader {
        let reader = Reader {
            stop: Arc::default(),
            closed: Arc::default(),
        };
        let (stop, closed) = (reader.stop.clone(), reader.closed.clone());
        std::thread::spawn(move || {
            let mut backoff = BACKOFF;
            while !closed.load(Ordering::SeqCst) {
                match open(&base, &query) {
                    Ok(conn) => {
                        *stop.lock().unwrap_or_else(|e| e.into_inner()) = Some(conn.stop.clone());
                        if closed.load(Ordering::SeqCst) {
                            break;
                        }
                        let _ = read_frames(conn.reader, |frame| {
                            backoff = BACKOFF;
                            on(Some(Wire::decode(&frame)));
                        });
                    }
                    Err(e) => eprintln!("events: {e}"),
                }
                if closed.load(Ordering::SeqCst) {
                    break;
                }
                on(None);
                std::thread::sleep(backoff);
                backoff = (backoff * 2).min(BACKOFF_MAX);
            }
        });
        reader
    }

    pub fn close(&self) {
        self.closed.store(true, Ordering::SeqCst);
        if let Some(stop) = self.stop.lock().unwrap_or_else(|e| e.into_inner()).take() {
            stop.stop();
        }
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    use std::net::TcpListener;
    use std::sync::mpsc;

    /// A loopback server that sends one frame and then holds the connection open until told to close.
    fn serve_one_frame() -> (u16, mpsc::Sender<()>) {
        let listener = TcpListener::bind("127.0.0.1:0").unwrap();
        let port = listener.local_addr().unwrap().port();
        let (release, released) = mpsc::channel::<()>();
        std::thread::spawn(move || {
            let (mut sock, _) = listener.accept().unwrap();
            let mut request = BufReader::new(sock.try_clone().unwrap());
            let mut line = String::new();
            while request.read_line(&mut line).unwrap() > 0 && line != "\r\n" {
                line.clear();
            }
            sock.write_all(
                b"HTTP/1.0 200 OK\r\nContent-Type: text/event-stream\r\n\r\nevent: hello\ndata: {\"buildIdentity\":\"x\"}\n\n",
            )
            .unwrap();
            sock.flush().unwrap();
            let _ = released.recv_timeout(Duration::from_secs(10));
        });
        (port, release)
    }

    #[test]
    fn stop_interrupts_a_blocked_read() {
        let (port, release) = serve_one_frame();
        let conn = open(&format!("http://127.0.0.1:{port}"), "agents=").unwrap();
        let stop = conn.stop.clone();
        let (frames_tx, frames_rx) = mpsc::channel();
        let (done_tx, done_rx) = mpsc::channel();
        std::thread::spawn(move || {
            let r = read_frames(conn.reader, |f| frames_tx.send(f).unwrap());
            done_tx.send(r.is_ok()).unwrap();
        });
        let first = frames_rx.recv_timeout(Duration::from_secs(5)).unwrap();
        assert_eq!(first.event, "hello");
        // The server is still holding the socket open, so the reader is blocked in read_line.
        stop.stop();
        let ended = done_rx.recv_timeout(Duration::from_secs(2));
        assert!(ended.is_ok(), "reader did not return within 2 s of stop()");
        let _ = release.send(());
    }

    fn wire(event: &str, data: &str) -> Wire {
        Wire::decode(&Frame {
            event: event.into(),
            data: data.into(),
        })
    }

    #[test]
    fn every_used_event_type_decodes() {
        assert!(
            matches!(wire("hello", r#"{"buildIdentity":"b1"}"#), Wire::Hello { build_identity } if build_identity == "b1")
        );
        assert!(matches!(
            wire("fleet", r#"{"workspaces":[],"unplaced":[]}"#),
            Wire::Fleet(_)
        ));
        assert!(matches!(wire("ping", ""), Wire::Ping));
        assert!(matches!(
            wire("state-changed", r#"{"namespace":"spaces.members","rev":7}"#),
            Wire::StateChanged { namespace, rev: 7 } if namespace == "spaces.members"
        ));
        assert!(matches!(
            wire("entry:mupu", r#"{"byteOffset":42,"kind":"assistant_text","payload":{}}"#),
            Wire::Entry { agent, entry } if agent == "mupu" && entry.byte_offset == 42
        ));
        assert!(
            matches!(wire("rewindow", r#"{"agent":"mupu"}"#), Wire::Rewindow { agent } if agent == "mupu")
        );
        assert!(matches!(wire("substrate", "{}"), Wire::Other(e) if e == "substrate"));
        assert!(matches!(wire("hello", "not json"), Wire::Other(e) if e == "hello"));
    }
}
