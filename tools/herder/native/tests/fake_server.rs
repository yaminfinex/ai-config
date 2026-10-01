//! The write path and the reconnect, against an in-process fake herder serve on loopback. Nothing here
//! talks to the real serve: web shares its state, and a bad write would show up in the owner's browser.

use herder_native::api::client::{Client, Page};
use herder_native::api::sse::Reader;
use herder_native::api::{StateRow, Wire};
use herder_native::local::{self, Disk};
use herder_native::shell::{save_then_message, save_then_send};
use herder_native::store::sync::{Hold, Ns, Step};
use herder_native::store::{Effect, Event, Fetch, Store, StreamEvent};
use serde_json::json;
use std::io::{BufRead, BufReader, Read, Write as _};
use std::net::TcpListener;
use std::path::PathBuf;
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

/// Run the store's effects the way the shell does, synchronously against the fake, until nothing is
/// left to do. A batch with sends goes through the shell's own `save_then_send`. A batch that only
/// persists is left unsaved, as if its task were still waiting, so every test also shows that a send
/// never depends on an earlier save having landed.
fn drive(store: &mut Store, client: &Client, disk: &Disk, first: Event) {
    let mut events = vec![first];
    while let Some(event) = events.pop() {
        let mut sends = Vec::new();
        for effect in store.apply(event) {
            match effect {
                Effect::Fetch(Fetch::State { ns, since }) => {
                    let step = match client.state(ns.name(), since) {
                        Ok(rows) => Step::Pulled(rows),
                        Err(e) => Step::PullFailed(e.status()),
                    };
                    events.push(Event::Sync { ns, step });
                }
                Effect::Send(write) => sends.push(write),
                _ => {}
            }
        }
        if !sends.is_empty() {
            let bytes = local::encode(&store.outbox());
            save_then_send(disk, client, &bytes, local::next_seq(), sends, |e| {
                events.push(e)
            });
        }
    }
}

/// A fresh local state directory for one test.
fn scratch(tag: &str) -> (Disk, PathBuf) {
    let dir = std::env::temp_dir().join(format!("herder-native-{tag}-{}", std::process::id()));
    let _ = std::fs::remove_dir_all(&dir);
    (Disk::at(dir.clone()), dir)
}

fn edit(key: &str, updated: i64) -> Event {
    Event::Sync {
        ns: Ns::Spaces,
        step: Step::Edit(vec![StateRow {
            key: key.into(),
            value: json!({"id": key, "name": key, "order": 2}),
            updated,
            write_id: "native".into(),
            deleted: false,
        }]),
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
    let (disk, dir) = scratch("posted");
    let mut store = Store::default();
    drive(&mut store, &client, &disk, edit("s2", 7));
    std::fs::remove_dir_all(dir).unwrap();

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

    let (disk, dir) = scratch("refusals");
    let mut store = Store::default();
    drive(&mut store, &client, &disk, edit("s2", 7));
    assert_eq!(store.sync[&Ns::Spaces].hold, Some(Hold::LocalOnly));

    *status.lock().unwrap() = 413;
    let mut store = Store::default();
    drive(&mut store, &client, &disk, edit("s2", 7));
    std::fs::remove_dir_all(dir).unwrap();
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
    let (disk, _) = scratch("reconnect"); // Nothing is edited, so nothing is saved.
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
                let step = Step::Pulled(rows);
                store.apply(Event::Sync { ns, step });
            }
            _ => {}
        }
    }
    let mut seen = Vec::new();
    while seen.iter().filter(|e| matches!(e, Wire::Hello(_))).count() < 2 {
        let event = rx
            .recv_timeout(Duration::from_secs(5))
            .expect("the stream reconnects");
        if let Event::Stream { event, .. } = &event {
            match event {
                StreamEvent::Frame(w) => seen.push(w.clone()),
                StreamEvent::Dropped => seen.push(Wire::Other("dropped".into())),
            }
        }
        drive(&mut store, &client, &disk, event);
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
        6,
        "each hello (the first too) re-pulls every namespace from its cursor: {log:?}"
    );
    assert_eq!(store.spaces.len(), 1);
}

/// Which version of row `s1` a JSON text holds (`updated`, compact or pretty), 0 for none.
fn version(text: &str) -> i64 {
    let rows: serde_json::Value = serde_json::from_str(text).unwrap_or_default();
    let rows = rows
        .get("rows")
        .cloned()
        .or_else(|| rows.get("spaces").cloned());
    rows.and_then(|r| r.as_array()?.first()?.get("updated")?.as_i64())
        .unwrap_or(0)
}

/// The race behind the durability rule: v2 is edited while v1's POST is in flight, and v2's own save
/// never runs. v1's answer leads to a pull, whose answer sends v2; that send must save v2 first.
#[test]
fn every_send_saves_the_outbox_it_is_sending_first() {
    let (disk, dir) = scratch("barrier");
    let outbox = dir.join(local::OUTBOX);
    let posts: Arc<Mutex<Vec<(i64, i64)>>> = Arc::default();
    let p2 = posts.clone();
    let (base, _) = serve(move |target, body| {
        if target.starts_with("POST /api/state/spaces") {
            let on_disk = std::fs::read_to_string(&outbox).unwrap_or_default();
            p2.lock().unwrap().push((version(body), version(&on_disk)));
            Reply::Json(200, json!({"accepted": [], "rev": 1}).to_string())
        } else {
            Reply::Json(200, json!({"rows": [], "rev": 1}).to_string())
        }
    });
    let client = Client::new(base);
    let mut store = Store::default();

    // v1: the shell encodes the outbox as it is now, then saves and posts on a background task.
    let sends: Vec<_> = store
        .apply(edit("s1", 1))
        .into_iter()
        .filter_map(|e| match e {
            Effect::Send(w) => Some(w),
            _ => None,
        })
        .collect();
    let (bytes, seq) = (local::encode(&store.outbox()), local::next_seq());
    // v2 arrives before that task runs. It cannot send yet, and its own save never lands.
    let v2 = store.apply(edit("s1", 2));
    assert!(v2.iter().all(|e| !matches!(e, Effect::Send(_))), "{v2:?}");

    let mut events = Vec::new();
    save_then_send(&disk, &client, &bytes, seq, sends, |e| events.push(e));
    for e in events {
        drive(&mut store, &client, &disk, e);
    }
    assert_eq!(
        *posts.lock().unwrap(),
        [(1, 1), (2, 2)],
        "(posted, on disk)"
    );
    assert!(store.sync[&Ns::Spaces].outbox.is_empty());
    std::fs::remove_dir_all(dir).unwrap();
}

/// A save the disk refuses posts nothing, and the send backs off to try again (saving first again).
#[test]
fn a_failed_save_posts_nothing_and_retries() {
    let (_, dir) = scratch("refused-save");
    std::fs::write(&dir, "a file where the directory should be").unwrap();
    let disk = Disk::at(dir.join("state"));
    let (base, log) = serve(|_, _| Reply::Json(200, json!({"rows": [], "rev": 1}).to_string()));
    let client = Client::new(base);
    let mut store = Store::default();
    let sends: Vec<_> = store
        .apply(edit("s1", 1))
        .into_iter()
        .filter_map(|e| match e {
            Effect::Send(w) => Some(w),
            _ => None,
        })
        .collect();
    let bytes = local::encode(&store.outbox());
    let mut events = Vec::new();
    save_then_send(&disk, &client, &bytes, local::next_seq(), sends, |e| {
        events.push(e)
    });

    assert!(log.lock().unwrap().is_empty(), "nothing reached the server");
    let [
        Event::Sync {
            ns: Ns::Spaces,
            step: Step::PostFailed(None),
        },
    ] = &events[..]
    else {
        panic!("{events:?}")
    };
    let effects = store.apply(events.pop().unwrap());
    assert!(
        matches!(effects[..], [Effect::Retry { after_ms: 500, .. }]),
        "{effects:?}"
    );
    assert_eq!(
        store.sync[&Ns::Spaces].outbox.len(),
        1,
        "the edit stays queued"
    );
    std::fs::remove_file(dir).unwrap();
}

/// U4: `POST /api/agents/{name}/message` with exactly `{text}`, after the prefs are saved; each
/// refusal status maps to its failure, and a failed save posts nothing.
#[test]
fn a_message_saves_the_draft_then_posts_once_and_maps_refusals() {
    use herder_native::store::composer::{Failure, Step as C};
    let refusal = |status: u16, error: &str| {
        let body = json!({"error": error, "detail": format!("{error} detail")});
        Reply::Json(status, body.to_string())
    };
    let (base, log) = serve(move |target, _| match target.split('/').nth(3) {
        Some("ok") => Reply::Json(
            200,
            json!({"sent": true, "to": "ok", "from": "web-x", "intent": "request"}).to_string(),
        ),
        Some("gone") => refusal(404, "agent not found"),
        Some("refuse") => refusal(409, "sender refused"),
        Some("down") => refusal(502, "substrate unreachable"),
        _ => refusal(400, "bad body"),
    });
    let client = Client::new(base);
    let (disk, dir) = scratch("message");
    let prefs = br#"{"drafts": {"ok": "hello \"there\""}}"#;
    let send = |agent: &str| {
        let text = "hello \"there\"".to_string();
        match save_then_message(
            &disk,
            &client,
            prefs,
            local::next_seq(),
            (agent.into(), text),
        ) {
            Event::Compose(C::Sent { agent: a, result }) if a == agent => result,
            other => panic!("{other:?}"),
        }
    };
    assert_eq!(send("ok"), Ok(()));
    assert_eq!(send("gone"), Err(Failure::UnknownAgent));
    assert_eq!(
        send("refuse"),
        Err(Failure::Refused("sender refused detail".into()))
    );
    assert_eq!(
        send("down"),
        Err(Failure::Unreachable("substrate unreachable detail".into()))
    );
    assert_eq!(
        send("odd"),
        Err(Failure::Rejected(400, "bad body detail".into()))
    );
    let log = log.lock().unwrap().clone();
    assert_eq!(log.len(), 5, "one POST each: {log:?}");
    assert_eq!(
        log[0],
        r#"POST /api/agents/ok/message {"text":"hello \"there\""}"#
    );
    assert_eq!(std::fs::read(dir.join(local::PREFS)).unwrap(), prefs);

    // The prefs cannot be saved: nothing is posted.
    let (_, blocked) = scratch("message-blocked");
    std::fs::write(&blocked, "a file where the directory should be").unwrap();
    let (base, log) = serve(|_, _| Reply::Json(200, "{}".into()));
    let event = save_then_message(
        &Disk::at(blocked.join("state")),
        &Client::new(base),
        prefs,
        local::next_seq(),
        ("ok".into(), "hi".into()),
    );
    let Event::Compose(C::Sent {
        result: Err(Failure::NotSaved(_)),
        ..
    }) = event
    else {
        panic!("{event:?}")
    };
    assert!(log.lock().unwrap().is_empty(), "nothing reached the server");
    std::fs::remove_file(blocked).unwrap();
}

/// A send nobody answered may have landed: it is reported as such, never retried.
#[test]
fn a_message_without_an_answer_is_not_retried() {
    use herder_native::store::composer::{Failure, Step as C};
    let listener = TcpListener::bind("127.0.0.1:0").unwrap();
    let base = format!("http://{}", listener.local_addr().unwrap());
    let accepted = Arc::new(Mutex::new(0));
    let count = accepted.clone();
    std::thread::spawn(move || {
        for sock in listener.incoming().flatten() {
            *count.lock().unwrap() += 1;
            drop(sock); // Hang up without an answer.
        }
    });
    let (disk, _) = scratch("message-hangup");
    let event = save_then_message(
        &disk,
        &Client::new(base),
        b"{}",
        local::next_seq(),
        ("a".into(), "hi".into()),
    );
    let Event::Compose(C::Sent {
        result: Err(Failure::NoAnswer(_)),
        ..
    }) = event
    else {
        panic!("{event:?}")
    };
    std::thread::sleep(Duration::from_millis(100));
    assert_eq!(*accepted.lock().unwrap(), 1, "exactly one attempt");
}
