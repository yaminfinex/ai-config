//! The `/api/events` stream: `event: <type>\ndata: <json>\n\n` frames with no `id:` line, so nothing
//! can be resumed; after a reconnect the client re-reads from REST.
//!
//! `Reader` is the stream's own thread. It speaks plain HTTP/1.0 over a `TcpStream` (the server is plain
//! HTTP, and 1.0 keeps the body unchunked), so an idle stream costs no CPU. It reopens with backoff
//! 500 ms doubling to 10 s, and the 45 s read timeout is the watchdog (the server pings every 15 s).
//! `close` (or dropping the `Reader`) ends it promptly wherever it is: the socket is published as soon
//! as it connects, so shutting it down interrupts the header read as well as a blocked frame read, and
//! the backoff sleep waits on a condvar. Only the connect itself (bounded at 5 s) is not interruptible.

use crate::api::types::Wire;
use std::io::{self, BufRead, BufReader, Write};
use std::net::{Shutdown, TcpStream, ToSocketAddrs};
use std::sync::{Arc, Condvar, Mutex, MutexGuard};
use std::time::Duration;

/// One server-sent event. `event` is `message` when the frame had no `event:` line.
#[derive(Clone, Debug, Default, PartialEq, Eq)]
pub struct Frame {
    pub event: String,
    pub data: String,
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

#[derive(Clone, Copy)]
struct Timing {
    backoff: Duration,
    backoff_max: Duration,
    watchdog: Duration,
}

const TIMING: Timing = Timing {
    backoff: Duration::from_millis(500),
    backoff_max: Duration::from_secs(10),
    watchdog: Duration::from_secs(45),
};

/// Connect to `http://host:port` with the watchdog as the read timeout. Returns the `Host` header too.
fn connect(base_url: &str, watchdog: Duration) -> io::Result<(TcpStream, String)> {
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
    stream.set_read_timeout(Some(watchdog))?;
    Ok((stream, host_port.to_string()))
}

/// Send `GET /api/events?{query}` and read past the headers. Fails on anything but a 200.
fn handshake(stream: TcpStream, host: &str, query: &str) -> io::Result<BufReader<TcpStream>> {
    let mut writer = stream.try_clone()?;
    write!(
        writer,
        "GET /api/events?{query} HTTP/1.0\r\nHost: {host}\r\nAccept: text/event-stream\r\n\r\n"
    )?;
    let mut reader = BufReader::with_capacity(1 << 16, stream);
    let mut line = String::new();
    reader.read_line(&mut line)?;
    if !line.contains(" 200 ") {
        return Err(io::Error::other(format!("events: {}", line.trim())));
    }
    line.clear();
    while reader.read_line(&mut line)? > 0 && !line.trim_end_matches(['\r', '\n']).is_empty() {
        line.clear();
    }
    Ok(reader)
}

#[derive(Default)]
struct Shared {
    closed: bool,
    /// A clone of the live socket; shutting it down unblocks whatever read the thread is in.
    socket: Option<TcpStream>,
}

type Cell = Arc<(Mutex<Shared>, Condvar)>;

fn lock(cell: &Cell) -> MutexGuard<'_, Shared> {
    cell.0.lock().unwrap_or_else(|e| e.into_inner())
}

/// The stream's thread: open, read frames until the connection ends, report the drop, back off, reopen.
/// A frame racing `close` can still be reported; the store's stream generation drops it.
pub struct Reader(Cell);

impl Reader {
    /// `on(Some(wire))` for each frame, `on(None)` each time a connection ends or fails to open.
    pub fn spawn(
        base: String,
        query: String,
        on: impl FnMut(Option<Wire>) + Send + 'static,
    ) -> Reader {
        Reader::spawn_with(base, query, TIMING, on)
    }

    fn spawn_with(
        base: String,
        query: String,
        t: Timing,
        mut on: impl FnMut(Option<Wire>) + Send + 'static,
    ) -> Reader {
        let cell: Cell = Arc::default();
        let shared = cell.clone();
        std::thread::spawn(move || {
            let mut backoff = t.backoff;
            loop {
                let opened = connect(&base, t.watchdog).and_then(|(sock, host)| {
                    let mut s = lock(&shared);
                    if s.closed {
                        return Ok(());
                    }
                    s.socket = Some(sock.try_clone()?);
                    drop(s);
                    let reader = handshake(sock, &host, &query)?;
                    read_frames(reader, |frame| {
                        backoff = t.backoff;
                        on(Some(Wire::decode(&frame.event, &frame.data)));
                    })
                });
                if let Err(e) = opened {
                    eprintln!("events: {e}");
                }
                if lock(&shared).closed {
                    return;
                }
                on(None);
                let s = lock(&shared);
                let (mut s, _) = shared
                    .1
                    .wait_timeout_while(s, backoff, |s| !s.closed)
                    .unwrap_or_else(|e| e.into_inner());
                if s.closed {
                    return;
                }
                s.socket = None;
                backoff = (backoff * 2).min(t.backoff_max);
            }
        });
        Reader(cell)
    }

    pub fn close(&self) {
        let mut s = lock(&self.0);
        s.closed = true;
        if let Some(sock) = s.socket.take() {
            let _ = sock.shutdown(Shutdown::Both);
        }
        self.0.1.notify_all();
    }
}

impl Drop for Reader {
    fn drop(&mut self) {
        self.close();
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    use std::net::TcpListener;
    use std::sync::mpsc;

    const FRAME: &[u8] =
        b"HTTP/1.0 200 OK\r\n\r\nevent: hello\ndata: {\"buildIdentity\":\"x\"}\n\n";

    /// A loopback server: each accepted connection is handed to `answer`, which may hold it open.
    fn serve(answer: impl Fn(TcpStream) + Send + Sync + 'static) -> String {
        let listener = TcpListener::bind("127.0.0.1:0").unwrap();
        let base = format!("http://{}", listener.local_addr().unwrap());
        let answer = Arc::new(answer);
        std::thread::spawn(move || {
            for sock in listener.incoming().flatten() {
                let answer = answer.clone();
                std::thread::spawn(move || answer(sock));
            }
        });
        base
    }

    /// Spawn a reader whose reports arrive on the channel. The channel disconnects when the thread
    /// ends (it owns the only sender), which is how the tests see that `close` really stopped it.
    fn reader(base: String, t: Timing) -> (Reader, mpsc::Receiver<Option<String>>) {
        let (tx, rx) = mpsc::channel();
        let r = Reader::spawn_with(base, "agents=".into(), t, move |w| {
            let _ = tx.send(w.map(|w| format!("{w:?}")));
        });
        (r, rx)
    }

    fn ends_promptly(rx: &mpsc::Receiver<Option<String>>) -> bool {
        loop {
            match rx.recv_timeout(Duration::from_secs(2)) {
                Ok(_) => continue,
                Err(mpsc::RecvTimeoutError::Disconnected) => return true,
                Err(mpsc::RecvTimeoutError::Timeout) => return false,
            }
        }
    }

    fn hold(mut sock: TcpStream, reply: &[u8]) {
        let mut line = String::new();
        let mut req = BufReader::new(sock.try_clone().unwrap());
        while req.read_line(&mut line).unwrap_or(0) > 0 && line != "\r\n" {
            line.clear();
        }
        let _ = sock.write_all(reply);
        std::thread::sleep(Duration::from_secs(30));
    }

    #[test]
    fn close_interrupts_a_blocked_frame_read() {
        let base = serve(|sock| hold(sock, FRAME));
        let (r, rx) = reader(base, TIMING);
        let first = rx.recv_timeout(Duration::from_secs(5)).unwrap();
        assert!(first.unwrap().starts_with("Hello"));
        r.close();
        assert!(ends_promptly(&rx), "the thread outlived close()");
    }

    #[test]
    fn close_interrupts_a_stalled_header_read() {
        // The server accepts and never answers, so the thread sits in the header read.
        let base = serve(|sock| hold(sock, b""));
        let (r, rx) = reader(base, TIMING);
        std::thread::sleep(Duration::from_millis(200));
        drop(r);
        assert!(ends_promptly(&rx), "a drop did not stop the header read");
    }

    #[test]
    fn close_interrupts_the_backoff() {
        let listener = TcpListener::bind("127.0.0.1:0").unwrap();
        let base = format!("http://{}", listener.local_addr().unwrap());
        drop(listener); // Nothing listens: every connect is refused.
        let long = Timing {
            backoff: Duration::from_secs(30),
            ..TIMING
        };
        let (r, rx) = reader(base, long);
        assert_eq!(rx.recv_timeout(Duration::from_secs(5)).unwrap(), None);
        r.close();
        assert!(ends_promptly(&rx), "close waited out the backoff");
    }

    #[test]
    fn a_silent_stream_trips_the_watchdog_and_reconnects() {
        let opened = Arc::new(Mutex::new(0));
        let o = opened.clone();
        let base = serve(move |sock| {
            *o.lock().unwrap() += 1;
            hold(sock, FRAME);
        });
        let quick = Timing {
            backoff: Duration::from_millis(50),
            watchdog: Duration::from_millis(300),
            ..TIMING
        };
        let (r, rx) = reader(base, quick);
        let got: Vec<bool> = (0..3)
            .map(|_| rx.recv_timeout(Duration::from_secs(5)).unwrap().is_some())
            .collect();
        assert_eq!(
            got,
            [true, false, true],
            "frame, watchdog drop, frame again"
        );
        assert_eq!(*opened.lock().unwrap(), 2);
        drop(r);
    }

    #[test]
    fn every_used_event_type_decodes() {
        let wire = Wire::decode;
        assert!(
            matches!(wire("hello", r#"{"buildIdentity":"b1"}"#), Wire::Hello(h) if h.build_identity == "b1")
        );
        assert!(matches!(
            wire("fleet", r#"{"workspaces":[],"unplaced":[]}"#),
            Wire::Fleet(_)
        ));
        assert!(matches!(wire("ping", ""), Wire::Ping));
        assert!(matches!(
            wire("state-changed", r#"{"namespace":"spaces.members","rev":7}"#),
            Wire::StateChanged(s) if s.namespace == "spaces.members" && s.rev == 7
        ));
        assert!(matches!(
            wire("entry:mupu", r#"{"byteOffset":42,"kind":"assistant_text","payload":{}}"#),
            Wire::Entry { agent, entry } if agent == "mupu" && entry.byte_offset == 42
        ));
        assert!(
            matches!(wire("rewindow", r#"{"agent":"mupu"}"#), Wire::Rewindow(r) if r.agent == "mupu")
        );
        assert!(matches!(wire("substrate", "{}"), Wire::Other(e) if e == "substrate"));
        assert!(matches!(wire("hello", "not json"), Wire::Other(e) if e == "hello"));
    }
}
