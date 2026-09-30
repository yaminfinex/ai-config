//! The write path and the reconnect, against an in-process fake herder serve on loopback. Nothing here
//! talks to the real serve: web shares its state, and a bad write would show up in the owner's browser.

use herder_native::api::client::{Client, Page};
use herder_native::api::sse::Reader;
use herder_native::api::{StateRow, Wire};
use herder_native::store::sync::{Hold, Ns};
use herder_native::store::{Effect, Event, Fetch, Store, StreamEvent, Write};
use serde_json::json;
use std::io::{BufRead, BufReader, Read, Write as _};
use std::net::TcpListener;
use std::sync::{Arc, Mutex, mpsc};
use std::time::Duration;

/// What the fake answers: a plain reply, or an event stream that ends or stays open.
enum Reply {
    Json(u16, String),
    Events { frames: String, hold: bool },
}

/// Starts the fake; every request is logged as `"METHOD /path?query body"`.
fn serve(
    answer: impl Fn(&str, &str) -> Reply + Send + Sync + 'static,
) -> (String, Arc<Mutex<Vec<String>>>) {
    let listener = TcpListener::bind("127.0.0.1:0").unwrap();
    let base = format!("http://{}", listener.local_addr().unwrap());
    let log: Arc<Mutex<Vec<String>>> = Arc::default();
    let (answer, log2) = (Arc::new(answer), log.clone());
    std::thread::spawn(move || {
        for sock in listener.incoming().flatten() {
            let (answer, log) = (answer.clone(), log2.clone());
            std::thread::spawn(move || {
                let mut r = BufReader::new(sock.try_clone().unwrap());
                let mut line = String::new();
                r.read_line(&mut line).unwrap();
                let target = line
                    .split_whitespace()
                    .take(2)
                    .collect::<Vec<_>>()
                    .join(" ");
                let mut len = 0;
                loop {
                    let mut h = String::new();
                    r.read_line(&mut h).unwrap();
                    if h.trim().is_empty() {
                        break;
                    }
                    if let Some(v) = h.to_ascii_lowercase().strip_prefix("content-length:") {
                        len = v.trim().parse().unwrap();
                    }
                }
                let mut body = vec![0; len];
                r.read_exact(&mut body).unwrap();
                let body = String::from_utf8(body).unwrap();
                log.lock()
                    .unwrap()
                    .push(format!("{target} {body}").trim().to_string());
                let mut sock = sock;
                match answer(&target, &body) {
                    Reply::Json(status, json) => {
                        let _ = write!(
                            sock,
                            "HTTP/1.1 {status} X\r\nContent-Type: application/json\r\nContent-Length: {}\r\nConnection: close\r\n\r\n{json}",
                            json.len()
                        );
                    }
                    Reply::Events { frames, hold } => {
                        let _ = write!(
                            sock,
                            "HTTP/1.0 200 OK\r\nContent-Type: text/event-stream\r\n\r\n{frames}"
                        );
                        let _ = sock.flush();
                        if hold {
                            std::thread::sleep(Duration::from_secs(30));
                        }
                    }
                }
            });
        }
    });
    (base, log)
}

fn rows_json(rev: u64) -> String {
    json!({"rows": [{"key": "s1", "value": {"id": "s1", "name": "one", "order": 1}, "updated": 5, "writeID": "w", "deleted": false}], "rev": rev})
        .to_string()
}

/// Run the store's network effects synchronously against the fake until nothing is left to do.
fn drive(store: &mut Store, client: &Client, first: Event) {
    let mut events = vec![first];
    while let Some(event) = events.pop() {
        for effect in store.apply(event) {
            match effect {
                Effect::Fetch(Fetch::State { ns, since }) => {
                    events.push(match client.state(ns.name(), since) {
                        Ok(rows) => Event::Pulled { ns, rows },
                        Err(e) => Event::PullFailed {
                            ns,
                            status: e.status(),
                        },
                    })
                }
                Effect::Send(Write::State { ns, rows }) => {
                    events.push(match client.post_state(ns.name(), &rows) {
                        Ok(_) => Event::Posted { ns },
                        Err(e) => Event::PostFailed {
                            ns,
                            status: e.status(),
                        },
                    })
                }
                _ => {}
            }
        }
    }
}

fn edit(key: &str, updated: i64) -> Event {
    Event::Edit {
        ns: Ns::Spaces,
        rows: vec![StateRow {
            key: key.into(),
            value: json!({"id": key, "name": key, "order": 2}),
            updated,
            write_id: "native".into(),
            deleted: false,
        }],
    }
}

#[test]
fn an_edit_is_posted_then_pulled_and_retired() {
    let (base, log) = serve(|target, _| match target {
        t if t.starts_with("POST /api/state/spaces") => {
            Reply::Json(200, r#"{"accepted":["s2"],"rev":6}"#.into())
        }
        t if t.starts_with("GET /api/state/spaces?") => Reply::Json(200, rows_json(6)),
        _ => Reply::Json(404, r#"{"error":"nope","detail":""}"#.into()),
    });
    let client = Client::new(base);
    let mut store = Store::default();
    drive(&mut store, &client, edit("s2", 7));

    let log = log.lock().unwrap().clone();
    assert_eq!(log.len(), 2, "{log:?}");
    assert!(
        log[0].starts_with("POST /api/state/spaces {\"rows\":[{\"key\":\"s2\""),
        "{}",
        log[0]
    );
    assert!(log[0].contains("\"writeID\":\"native\""), "{}", log[0]);
    assert_eq!(log[1], "GET /api/state/spaces?since=0");
    assert!(store.sync[&Ns::Spaces].outbox.is_empty());
    let names: Vec<&str> = store.spaces.iter().map(|s| s.name.as_str()).collect();
    assert_eq!(names, ["one", "s2"]);
}

#[test]
fn refusals_map_to_holds_and_a_missing_namespace_is_empty() {
    let status = Arc::new(Mutex::new(409u16));
    let s2 = status.clone();
    let (base, _) = serve(move |target, _| match target {
        t if t.starts_with("POST") => Reply::Json(
            *s2.lock().unwrap(),
            r#"{"error":"refused","detail":"x"}"#.into(),
        ),
        _ => Reply::Json(
            404,
            r#"{"error":"state namespace not found","detail":""}"#.into(),
        ),
    });
    let client = Client::new(base);
    assert!(client.state("notes", 0).unwrap().rows.is_empty());

    let mut store = Store::default();
    drive(&mut store, &client, edit("s2", 7));
    assert_eq!(store.sync[&Ns::Spaces].hold, Some(Hold::LocalOnly));

    *status.lock().unwrap() = 413;
    let mut store = Store::default();
    drive(&mut store, &client, edit("s2", 7));
    assert_eq!(store.sync[&Ns::Spaces].hold, Some(Hold::TooLarge));
    assert_eq!(
        store.sync[&Ns::Spaces].outbox.len(),
        1,
        "a refused row is kept"
    );
}

#[test]
fn reads_and_sends_use_the_documented_shapes() {
    let (base, log) = serve(|target, _| {
        match target {
        t if t.starts_with("GET /api/viewer") => Reply::Json(200, r#"{"viewer":"web-me"}"#.into()),
        t if t.contains("/entries") => Reply::Json(200, r#"{"sessionId":"s","window":{"mode":"before","from":0,"limit":2},"entries":[],"prevOffset":0}"#.into()),
        t if t.starts_with("POST /api/agents/mupu/message") => Reply::Json(200, r#"{"sent":true}"#.into()),
        _ => Reply::Json(502, r#"{"error":"substrate unreachable","detail":""}"#.into()),
    }
    });
    let client = Client::new(base);
    assert_eq!(client.viewer().unwrap().viewer, "web-me");
    let page = Page::Before {
        offset: 42,
        session: "s".into(),
        limit: 2,
    };
    assert_eq!(client.entries("mupu", &page).unwrap().prev_offset, Some(0));
    client.send_message("mupu", "hi").unwrap();
    assert_eq!(client.fleet().unwrap_err().status(), Some(502));
    let log = log.lock().unwrap().clone();
    assert_eq!(
        log[1],
        "GET /api/agents/mupu/entries?before=42&sessionId=s&limit=2"
    );
    assert_eq!(log[2], r#"POST /api/agents/mupu/message {"text":"hi"}"#);
}

#[test]
fn a_dropped_stream_reconnects_and_repulls() {
    let opened = Arc::new(Mutex::new(0));
    let o2 = opened.clone();
    let (base, log) = serve(move |target, _| {
        if target.starts_with("GET /api/events") {
            let mut n = o2.lock().unwrap();
            *n += 1;
            let frames = "event: hello\ndata: {\"buildIdentity\":\"b1\"}\n\nevent: fleet\ndata: {\"workspaces\":[],\"unplaced\":[]}\n\n";
            // The first connection ends right after its frames; the second stays up.
            Reply::Events {
                frames: frames.into(),
                hold: *n > 1,
            }
        } else if target.starts_with("GET /api/state/") {
            Reply::Json(200, rows_json(6))
        } else {
            Reply::Json(404, "{}".into())
        }
    });
    let client = Client::new(base.clone());
    let mut store = Store::default();
    let (tx, rx) = mpsc::channel();
    let mut reader = None;
    for effect in store.apply(Event::Boot) {
        match effect {
            Effect::Stream { generation, agents } => {
                let tx = tx.clone();
                let query = format!("agents={}", agents.join(","));
                reader = Some(Reader::spawn(base.clone(), query, move |wire| {
                    let event = wire.map_or(StreamEvent::Dropped, StreamEvent::Frame);
                    let _ = tx.send(Event::Stream { generation, event });
                }));
            }
            Effect::Fetch(Fetch::State { ns, since }) => {
                let rows = client.state(ns.name(), since).unwrap();
                store.apply(Event::Pulled { ns, rows });
            }
            _ => {}
        }
    }
    let mut seen = Vec::new();
    while seen
        .iter()
        .filter(|e| matches!(e, Wire::Hello { .. }))
        .count()
        < 2
    {
        let event = rx
            .recv_timeout(Duration::from_secs(5))
            .expect("the stream reconnects");
        if let Event::Stream { event, .. } = &event {
            match event {
                StreamEvent::Frame(w) => seen.push(w.clone()),
                StreamEvent::Dropped => seen.push(Wire::Other("dropped".into())),
            }
        }
        drive(&mut store, &client, event);
    }
    reader.unwrap().close();

    assert!(
        matches!(seen[2], Wire::Other(ref d) if d == "dropped"),
        "{seen:?}"
    );
    let log = log.lock().unwrap().clone();
    let events = log
        .iter()
        .filter(|l| l.starts_with("GET /api/events"))
        .count();
    assert_eq!(events, 2, "{log:?}");
    let repulls: Vec<&String> = log.iter().filter(|l| l.contains("since=6")).collect();
    assert_eq!(
        repulls.len(),
        3,
        "every namespace re-pulled from its cursor: {log:?}"
    );
    assert_eq!(store.spaces.len(), 1);
}
