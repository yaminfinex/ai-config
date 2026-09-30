//! The `/api/events` stream: `event: <type>\ndata: <json>\n\n` frames with no `id:` line, so nothing
//! can be resumed; after a reconnect the client re-reads from REST. The socket read blocks on its own
//! thread, so an idle stream costs no CPU. Backoff 500 ms doubling to 10 s; a 45 s read timeout is
//! the watchdog (the server pings every 15 s).

use std::io::BufRead;

/// One server-sent event. `event` is `message` when the frame had no `event:` line.
#[derive(Clone, Debug, Default, PartialEq, Eq)]
pub struct Frame {
    pub event: String,
    pub data: String,
}

/// Reads frames until the reader ends or fails. `data:` lines may be many kilobytes (the whole board).
pub fn read_frames<R: BufRead>(mut reader: R, mut on: impl FnMut(Frame)) -> std::io::Result<()> {
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
