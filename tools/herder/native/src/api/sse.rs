//! The `/api/events` stream: `event: <type>\ndata: <json>\n\n` frames with no `id:` line, so nothing
//! can be resumed; after a reconnect the client re-reads from REST.
//!
//! The connection is a plain `TcpStream` speaking HTTP/1.0 (the server is plain HTTP, and 1.0 keeps the
//! body unchunked). The read blocks on its own thread, so an idle stream costs no CPU, and `Stop`
//! (a clone of the socket) shuts it down from any thread so a subscription change never waits for the
//! read timeout. Backoff 500 ms doubling to 10 s is the caller's; the 45 s read timeout is the watchdog
//! (the server pings every 15 s).

use std::io::{self, BufRead, BufReader, Write};
use std::net::{Shutdown, TcpStream, ToSocketAddrs};
use std::sync::Arc;
use std::time::Duration;

/// One server-sent event. `event` is `message` when the frame had no `event:` line.
#[derive(Clone, Debug, Default, PartialEq, Eq)]
pub struct Frame {
    pub event: String,
    pub data: String,
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
}
