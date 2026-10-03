//! The write path and the reconnect, against an in-process fake herder serve on loopback. Nothing here
//! talks to the real serve: web shares its state, and a bad write would show up in the owner's browser.

use herder_native::api::client::{Client, Page};
use herder_native::api::sse::Reader;
use herder_native::api::{StateRow, Wire};
use herder_native::local::{self, Disk};
use herder_native::shell::{Batch, save_then_land, save_then_message, save_then_send};
use herder_native::store::sync::{Hold, Ns, Step};
use herder_native::store::{Effect, Event, Fetch, Store, StreamEvent};
use serde_json::json;
use std::collections::VecDeque;
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
/// left to do; the events applied, in order (first in, first out, as they reach the shell's channel).
/// Writes are gathered by the shell's own `Batch`; the posts and queued notes waiting on the outbox go
/// through its `save_then_send`, a hand-off's draft through its `save_then_land`. A batch that only
/// persists is left unsaved, as if its task were still waiting, so every test also shows that a send
/// never depends on an earlier save having landed.
fn drive(store: &mut Store, client: &Client, disk: &Disk, first: Event) -> Vec<Event> {
    let (mut events, mut applied) = (VecDeque::from([first]), Vec::new());
    while let Some(event) = events.pop_front() {
        let mut batch = Batch::default();
        let effects = store.apply(event.clone());
        applied.push(event);
        for effect in effects.into_iter().filter_map(|e| batch.take(e)) {
            if let Effect::Fetch(Fetch::State { ns, since }) = effect {
                let step = match client.state(ns.name(), since) {
                    Ok(rows) => Step::Pulled(rows),
                    Err(e) => Step::PullFailed(e.status()),
                };
                events.push_back(Event::Sync { ns, step });
            }
        }
        for agent in std::mem::take(&mut batch.drafts) {
            let (bytes, seq) = (local::encode(&store.prefs), local::next_seq());
            events.push_back(save_then_land(disk, &bytes, seq, agent));
        }
        if batch.waits() {
            let (bytes, seq) = (local::encode(&store.outbox()), local::next_seq());
            save_then_send(disk, client, &bytes, seq, batch.sends, batch.lands, |e| {
                events.push_back(e)
            });
        }
    }
    applied
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
    assert_eq!(client.agent("gone").unwrap_err().status(), Some(502));
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
        8,
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
            Effect::Post { ns, rows } => Some((ns, rows)),
            _ => None,
        })
        .collect();
    let (bytes, seq) = (local::encode(&store.outbox()), local::next_seq());
    // v2 arrives before that task runs. It cannot send yet, and its own save never lands.
    let v2 = store.apply(edit("s1", 2));
    assert!(
        v2.iter().all(|e| !matches!(e, Effect::Post { .. })),
        "{v2:?}"
    );

    let mut events = Vec::new();
    save_then_send(&disk, &client, &bytes, seq, sends, vec![], |e| {
        events.push(e)
    });
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
            Effect::Post { ns, rows } => Some((ns, rows)),
            _ => None,
        })
        .collect();
    let bytes = local::encode(&store.outbox());
    let mut events = Vec::new();
    save_then_send(
        &disk,
        &client,
        &bytes,
        local::next_seq(),
        sends,
        vec![],
        |e| events.push(e),
    );

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
        matches!(effects[..], [Effect::After { after_ms: 500, .. }]),
        "{effects:?}"
    );
    assert_eq!(
        store.sync[&Ns::Spaces].outbox.len(),
        1,
        "the edit stays queued"
    );
    std::fs::remove_file(dir).unwrap();
}

/// U4: `POST /api/agents/{name}/message` with exactly `{text}`, after the prefs are saved (the fake
/// checks the file as the POST arrives); each refusal maps to its failure, and a failed save posts
/// nothing.
#[test]
fn a_message_saves_the_draft_then_posts_once_and_maps_refusals() {
    use herder_native::api::Refusal;
    use herder_native::store::composer::{Failure, Step as C};
    let refusal = |status: u16, error: &str| {
        let body = json!({"error": error, "detail": format!("{error} detail")});
        Reply::Json(status, body.to_string())
    };
    let (disk, dir) = scratch("message");
    let prefs = br#"{"drafts": {"ok": "hello \"there\""}}"#;
    let saved = dir.join(local::PREFS);
    assert!(!saved.exists());
    let (base, log) = serve(move |target, _| match target.split('/').nth(3) {
        Some("ok") if std::fs::read(&saved).ok().as_deref() == Some(&prefs[..]) => Reply::Json(
            200,
            json!({"sent": true, "to": "ok", "from": "web-x", "intent": "request"}).to_string(),
        ),
        Some("ok") => refusal(500, "posted before the prefs were saved"),
        Some("gone") => refusal(404, "agent not found"),
        Some("collide") => refusal(409, "sender refused"),
        Some("anon") => refusal(409, "attribution required"),
        Some("retired") => refusal(409, "retired agent"),
        Some("down") => refusal(502, "substrate unreachable"),
        _ => refusal(400, "bad body"),
    });
    let client = Client::new(base);
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
    let unattributed = |error: &str| {
        Err(Failure::Unattributed(Refusal {
            error: error.into(),
            detail: format!("{error} detail"),
        }))
    };
    assert_eq!(send("collide"), unattributed("sender refused"));
    assert_eq!(send("anon"), unattributed("attribution required"));
    assert_eq!(
        send("retired"),
        Err(Failure::Refused("retired agent detail".into()))
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
    assert_eq!(log.len(), 7, "one POST each: {log:?}");
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

/// F7: a quick send from the capture popover is in the prefs on disk before its POST (the fake reads
/// the file as it arrives). The app stopping there, before the answer (the fake hangs up), the next boot
/// finds it and adds it to the agent's draft, after what was there, without sending it again. A failed
/// save posts nothing.
#[test]
fn a_quick_send_is_saved_before_it_posts_and_a_restart_keeps_it_unsent() {
    use herder_native::store::Prefs;
    use herder_native::store::composer::{Failure, Step as C};
    let text = "from mupu's transcript:\n> the reducer\n\nok".to_string();
    let mut prefs = Prefs::default();
    prefs.drafts.insert("mupu".into(), "mine".into());
    prefs.quick.insert("mupu".into(), text.clone());
    let bytes = local::encode(&prefs);
    let (disk, dir) = scratch("quick");
    let saved = dir.join(local::PREFS);
    let want = bytes.clone();
    let (base, log) = serve(move |_, _| {
        let on_disk = std::fs::read(&saved).ok() == Some(want.clone());
        Reply::Json(if on_disk { 599 } else { 500 }, "{}".into())
    });
    let message = ("mupu".into(), text.clone());
    let event = save_then_message(
        &disk,
        &Client::new(base),
        &bytes,
        local::next_seq(),
        message,
    );
    let Event::Compose(C::Sent {
        result: Err(Failure::Rejected(599, _)),
        ..
    }) = event
    else {
        panic!("posted before the prefs were saved: {event:?}")
    };
    let log = log.lock().unwrap().clone();
    assert_eq!(log.len(), 1, "posted once: {log:?}");
    // The fake answered 599 only when the prefs were already on disk; the answer is dropped here, as if
    // the app had stopped. The next boot:
    let mut store = Store::default();
    let effects = store.apply(Event::PrefsLoaded(disk.load_prefs().unwrap()));
    assert_eq!(store.prefs.drafts["mupu"], format!("mine\n\n{text}"));
    assert!(store.prefs.quick.is_empty());
    assert!(
        !effects.iter().any(|e| matches!(e, Effect::Message { .. })),
        "{effects:?}"
    );
    assert!(effects.contains(&Effect::Persist(herder_native::store::Persist::Prefs)));
    std::fs::remove_dir_all(dir).unwrap();
    // The prefs cannot be saved: nothing is posted.
    let (_, blocked) = scratch("quick-blocked");
    std::fs::write(&blocked, "a file where the directory should be").unwrap();
    let (base, log) = serve(|_, _| Reply::Json(200, "{}".into()));
    let blocked_disk = Disk::at(blocked.join("state"));
    let message = ("mupu".into(), text);
    let event = save_then_message(
        &blocked_disk,
        &Client::new(base),
        &bytes,
        local::next_seq(),
        message,
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

/// Any 2xx is a send that landed, whatever its body: a malformed one or a 204's empty one. A state row
/// is retired by the POST (the pull does not hold it), and each send is posted once.
#[test]
fn a_2xx_without_a_json_body_counts_as_sent() {
    use herder_native::store::composer::Step as C;
    for (status, body) in [(200, "not json"), (204, "")] {
        let (base, log) = serve(move |target, _| match target {
            t if t.starts_with("POST") => Reply::Json(status, body.into()),
            _ => Reply::Json(200, rows_json(6)),
        });
        let client = Client::new(base);
        let (disk, dir) = scratch(&format!("odd-body-{status}"));
        let mut store = Store::default();
        drive(&mut store, &client, &disk, edit("s2", 7));
        assert!(
            store.sync[&Ns::Spaces].outbox.is_empty(),
            "{status}: retired"
        );
        assert_eq!(store.sync[&Ns::Spaces].hold, None, "{status}");
        let message = ("ok".into(), "hi".into());
        let event = save_then_message(&disk, &client, b"{}", local::next_seq(), message);
        let Event::Compose(C::Sent { result: Ok(()), .. }) = event else {
            panic!("{status}: {event:?}")
        };
        std::fs::remove_dir_all(dir).unwrap();
        let log = log.lock().unwrap().clone();
        let posts: Vec<&str> = (log.iter())
            .filter_map(|l| l.strip_prefix("POST ")?.split(' ').next())
            .collect();
        assert_eq!(
            posts,
            ["/api/state/spaces", "/api/agents/ok/message"],
            "{status}: {log:?}"
        );
    }
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

/// U5: a note added here is saved to `outbox.json` before its POST (the barrier), posted in web's
/// record shape, then pulled back and retired from the outbox.
#[test]
fn a_note_is_saved_then_posted_in_webs_shape_then_retired() {
    use herder_native::store::notes::{Stamp, Step as N};
    let (disk, dir) = scratch("note");
    let outbox = dir.join(local::OUTBOX);
    let posted: Arc<Mutex<Vec<(String, String)>>> = Arc::default();
    let p2 = posted.clone();
    let (base, log) = serve(move |target, body| {
        if target.starts_with("POST /api/state/notes") {
            let on_disk = std::fs::read_to_string(&outbox).unwrap_or_default();
            p2.lock().unwrap().push((body.to_string(), on_disk));
            Reply::Json(200, json!({"accepted": ["n1"], "rev": 3}).to_string())
        } else if target.starts_with("GET /api/state/notes") {
            let rows = p2.lock().unwrap().last().map(|(b, _)| b.clone());
            let rows: serde_json::Value = serde_json::from_str(&rows.unwrap_or_default()).unwrap();
            Reply::Json(200, json!({"rows": rows["rows"], "rev": 3}).to_string())
        } else {
            Reply::Json(404, r#"{"error":"nope","detail":""}"#.into())
        }
    });
    let client = Client::new(base);
    let mut store = Store::default();
    let stamp = Stamp {
        now: 1_790_000_000_000,
        id: "n1".into(),
        write: "w1".into(),
    };
    let add = N::Add {
        group: "mupu".into(),
        text: "ask about the tests".into(),
        quote: Some("cargo test".into()),
        stamp,
    };
    drive(&mut store, &client, &disk, Event::Note(add));
    std::fs::remove_dir_all(dir).unwrap();

    let posted = posted.lock().unwrap().clone();
    assert_eq!(posted.len(), 1, "{:?}", log.lock().unwrap());
    let (body, on_disk) = &posted[0];
    let body: serde_json::Value = serde_json::from_str(body).unwrap();
    let want = json!({"key": "n1", "updated": 1_790_000_000_000i64, "writeID": "w1", "deleted": false,
        "value": {"id": "n1", "group": "mupu", "text": "ask about the tests", "quote": "cargo test",
                  "source": {"kind": "transcript", "agent": "mupu"}, "created": 1_790_000_000_000i64}});
    assert_eq!(body["rows"], json!([want]));
    assert!(
        on_disk.contains("\"n1\""),
        "saved before the POST: {on_disk}"
    );
    assert!(store.sync[&Ns::Notes].outbox.is_empty());
    assert_eq!(store.notes_of("mupu").count(), 1);
}

/// U5: alt-enter's note is in `outbox.json` before the draft clears (it lands before the POST's answer,
/// and is on disk when the POST arrives); a disk that refuses keeps the draft, says why and posts
/// nothing.
#[test]
fn a_queued_draft_clears_only_once_its_note_is_saved() {
    use herder_native::store::notes::{Stamp, Step as N};
    for refused in [false, true] {
        let (_, dir) = scratch(&format!("queue-{refused}"));
        let disk = match refused {
            false => Disk::at(dir.clone()),
            true => {
                std::fs::write(&dir, "a file where the directory should be").unwrap();
                Disk::at(dir.join("state"))
            }
        };
        let (outbox, at_post) = (dir.join(local::OUTBOX), Arc::new(Mutex::new(Vec::new())));
        let at = at_post.clone();
        let (base, log) = serve(move |target, _| match target.starts_with("POST") {
            true => {
                let on_disk = std::fs::read_to_string(&outbox).unwrap_or_default();
                at.lock().unwrap().push(on_disk);
                Reply::Json(200, json!({"accepted": ["q1"], "rev": 2}).to_string())
            }
            false => Reply::Json(200, json!({"rows": [], "rev": 1}).to_string()),
        });
        let client = Client::new(base);
        let mut store = Store::default();
        store.prefs.drafts.insert("mupu".into(), "later".into());
        let stamp = Stamp {
            now: 7,
            id: "q1".into(),
            write: "w1".into(),
        };
        let queue = N::Queue {
            agent: "mupu".into(),
            stamp,
        };
        let applied = drive(&mut store, &client, &disk, Event::Note(queue));
        let landed = applied
            .iter()
            .position(|e| matches!(e, Event::Note(N::Landed { saved: Ok(()), .. })));
        let posted = applied.iter().position(|e| {
            matches!(
                e,
                Event::Sync {
                    ns: Ns::Notes,
                    step: Step::Posted
                }
            )
        });
        let posts = log
            .lock()
            .unwrap()
            .iter()
            .filter(|l| l.starts_with("POST"))
            .count();
        let note: Vec<_> = store.notes_of("mupu").map(|n| n.text.as_str()).collect();
        assert_eq!(note, ["later"], "the note is kept here either way");
        match refused {
            false => {
                assert_eq!(posts, 1);
                assert!(landed < posted && landed.is_some(), "{applied:?}");
                let at_post = at_post.lock().unwrap();
                assert!(at_post[0].contains("\"q1\""), "saved before the POST");
                assert!(!store.prefs.drafts.contains_key("mupu"));
                assert!(store.note_problems.is_empty());
                std::fs::remove_dir_all(dir).unwrap();
            }
            true => {
                assert_eq!(posts, 0, "nothing reached the server");
                assert!(landed.is_none() && posted.is_none(), "{applied:?}");
                assert_eq!(store.prefs.drafts["mupu"], "later");
                assert!(store.note_problems["mupu"].contains("the draft stays"));
                std::fs::remove_file(dir).unwrap();
            }
        }
    }
}

/// U5: with a notes POST already in flight, alt-enter's batch posts nothing, and its note still lands
/// only with the outbox's save: on disk before the draft clears.
#[test]
fn a_queued_note_lands_with_the_outbox_while_a_post_is_in_flight() {
    use herder_native::store::notes::{Stamp, Step as N};
    let (disk, dir) = scratch("queue-busy");
    let (base, log) = serve(|_, _| Reply::Json(200, json!({"rows": [], "rev": 1}).to_string()));
    let client = Client::new(base);
    let mut store = Store::default();
    store.prefs.drafts.insert("mupu".into(), "later".into());
    let stamp = |id: &str| Stamp {
        now: 7,
        id: id.into(),
        write: format!("w-{id}"),
    };
    let add = N::Add {
        group: "mupu".into(),
        text: "first".into(),
        quote: None,
        stamp: stamp("n1"),
    };
    let effects = store.apply(Event::Note(add));
    assert!(
        effects.iter().any(|e| matches!(e, Effect::Post { .. })),
        "n1's POST, never answered"
    );
    let queue = N::Queue {
        agent: "mupu".into(),
        stamp: stamp("q1"),
    };
    let applied = drive(&mut store, &client, &disk, Event::Note(queue));
    let on_disk = std::fs::read_to_string(dir.join(local::OUTBOX)).unwrap_or_default();
    std::fs::remove_dir_all(dir).unwrap();
    let landed = |e: &Event| matches!(e, Event::Note(N::Landed { saved: Ok(()), .. }));
    assert!(applied.iter().any(landed), "{applied:?}");
    assert!(
        on_disk.contains("\"q1\""),
        "landed with the outbox: {on_disk}"
    );
    assert!(!store.prefs.drafts.contains_key("mupu"));
    assert!(log.lock().unwrap().iter().all(|l| !l.starts_with("POST")));
}
