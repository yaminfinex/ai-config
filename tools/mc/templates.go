package main

const pageTpl = `
{{define "page"}}<!doctype html>
<html><head><meta charset="utf-8"><title>mc — {{.Page}}</title>
<meta name="viewport" content="width=device-width, initial-scale=1">
<style>
:root{--fg:#1a1a1a;--dim:#777;--line:#e2e2e2;--accent:#0b57d0;--bg:#fafaf8;--card:#fff;--warn:#b3261e}
*{box-sizing:border-box}
body{font:15px/1.5 system-ui,sans-serif;color:var(--fg);background:var(--bg);margin:0}
header{display:flex;gap:1.2em;align-items:baseline;padding:.7em 1.2em;border-bottom:1px solid var(--line);background:var(--card)}
header .brand{font-weight:700}
header a{color:var(--fg);text-decoration:none}
header a.on{color:var(--accent);font-weight:600}
header .who{margin-left:auto;color:var(--dim);font-size:.85em}
main{max-width:56em;margin:1.2em auto;padding:0 1.2em}
h2{font-size:1em;text-transform:uppercase;letter-spacing:.06em;color:var(--dim);margin:1.4em 0 .5em}
.card{background:var(--card);border:1px solid var(--line);border-radius:8px;padding:.8em 1em;margin:.5em 0}
.card a.title{font-weight:600;color:var(--fg);text-decoration:none}
.meta{color:var(--dim);font-size:.85em}
.badge{display:inline-block;font-size:.75em;padding:.05em .5em;border-radius:99px;border:1px solid var(--line);color:var(--dim);margin-right:.4em}
.badge.decide{border-color:#b3261e;color:#b3261e}
.badge.act{border-color:#7b4a00;color:#7b4a00}
.badge.reply{border-color:var(--accent);color:var(--accent)}
.badge.yourturn{background:var(--accent);border-color:var(--accent);color:#fff}
.err{background:#fdeceb;border:1px solid var(--warn);color:var(--warn);padding:.6em 1em;border-radius:8px;margin:.8em 0}
.ctx{background:#fffbe8;border:1px solid #e6d9a0;border-radius:8px;padding:.8em 1em;margin:.8em 0;white-space:pre-wrap}
.msg{border-left:3px solid var(--line);padding:.3em .9em;margin:.7em 0;white-space:pre-wrap}
.msg .from{font-weight:600;white-space:normal}
.msg.human{border-left-color:var(--accent)}
form.stack label{display:block;margin:.7em 0 .2em;font-size:.85em;color:var(--dim)}
input[type=text],textarea,select{width:100%;padding:.45em .6em;border:1px solid var(--line);border-radius:6px;font:inherit;background:#fff}
textarea{min-height:6em}
button{font:inherit;padding:.45em 1.1em;border:1px solid var(--accent);border-radius:6px;background:var(--accent);color:#fff;cursor:pointer;margin-top:.8em}
button.quiet{background:#fff;color:var(--fg);border-color:var(--line)}
.rowform{display:flex;gap:.6em;align-items:flex-start;margin-top:1em}
.rowform textarea{flex:1;min-height:3.4em}
.rowform button{margin-top:0}
details{margin:.8em 0}
summary{cursor:pointer;color:var(--dim)}
.empty{color:var(--dim);font-style:italic;padding:.6em 0}
table{border-collapse:collapse;width:100%}
td,th{text-align:left;padding:.3em .6em;border-bottom:1px solid var(--line);font-size:.9em}
th{color:var(--dim);font-weight:500}
.footer{color:var(--dim);font-size:.8em;margin:2em 0;text-align:center}
</style></head><body>
<header>
  <span class="brand">mc</span>
  <a href="/" {{if eq .Page "inbox"}}class="on"{{end}}>inbox</a>
  <a href="/threads" {{if eq .Page "threads"}}class="on"{{end}}>all threads</a>
  <a href="/roster" {{if eq .Page "roster"}}class="on"{{end}}>roster</a>
  <a href="/talk" {{if eq .Page "talk"}}class="on"{{end}}>talk to…</a>
  <span class="who">{{.User}} · seat @{{.Seat}}{{if .BusDir}} · LAB BUS{{end}}</span>
</header>
<main>
{{if .Error}}<div class="err">{{.Error}}</div>{{end}}

{{if eq .Page "inbox"}}
  <h2>Your turn</h2>
  {{range .YourTurn}}{{template "card" .}}{{else}}<div class="empty">nothing needs you</div>{{end}}
  <h2>Waiting on them</h2>
  {{range .Waiting}}{{template "card" .}}{{else}}<div class="empty">nothing in flight</div>{{end}}
  {{if .ShowClosed}}
    <h2>Closed</h2>
    {{range .Closed}}{{template "card" .}}{{else}}<div class="empty">none</div>{{end}}
    <p class="meta"><a href="/">hide closed</a></p>
  {{else}}
    <p class="meta"><a href="/?closed=1">show closed</a></p>
  {{end}}
{{end}}

{{if eq .Page "thread"}}{{with .T}}
  <p class="meta"><a href="/">&larr; inbox</a></p>
  <h1 style="margin:.2em 0">{{.Title}}</h1>
  <p class="meta">
    <span class="badge {{.Expects}}">{{.Expects}}</span>
    <span class="badge">{{.Weight}}</span>
    {{if eq .Grade "observed"}}<span class="badge">observed</span>{{else if eq .Status "closed"}}<span class="badge">closed</span>{{else if eq .Turn "owner"}}<span class="badge yourturn">your turn</span>{{else}}<span class="badge">waiting on {{.Turn}}</span>{{end}}
    opened by {{.OpenedBy}}{{with .With}} · with {{range $i, $w := .}}{{if $i}}, {{end}}{{$w}}{{end}}{{end}}
    {{with .Home}} · home {{.}}{{end}} · updated {{ago .Updated}} ago · id {{.ID}}
  </p>
  {{if eq .Grade "managed"}}
    <details>
      <summary>Retitle</summary>
      <form class="rowform" method="post" action="/thread/{{.ID}}/retitle">
        <input type="text" name="title" value="{{.Title}}" aria-label="New title">
        <button class="quiet">Save title</button>
      </form>
    </details>
  {{end}}
  <div class="ctx">{{.Context}}</div>
  {{if .Resolution}}<div class="card"><strong>Resolution:</strong> {{.Resolution}}</div>{{end}}
  {{range .Msgs}}
    <div class="msg{{if hasHumanPrefix .From}} human{{end}}"><span class="from">{{.From}}</span> <span class="meta">#{{.BusID}}{{with .Intent}} · {{.}}{{end}} · {{.TS}}</span>
{{.Text}}</div>
  {{else}}<div class="empty">no messages linked yet</div>{{end}}
  {{if eq .Grade "managed"}}
    {{if eq .Status "open"}}
      <form class="rowform" method="post" action="/thread/{{.ID}}/reply">
        <textarea name="text" placeholder="reply on this thread…"></textarea>
        <button>Send</button>
      </form>
      <form class="rowform" method="post" action="/thread/{{.ID}}/close">
        <textarea name="resolution" placeholder="resolution — required to close"></textarea>
        <button class="quiet">Close</button>
      </form>
    {{else}}
      <form method="post" action="/thread/{{.ID}}/reopen"><button class="quiet">Reopen</button></form>
    {{end}}
  {{end}}
{{end}}{{end}}

{{if eq .Page "threads"}}
  <h1>All threads</h1>
  <p class="meta">bus threads mc tracks but does not manage — an explicit @{{.Seat}} raise promotes one onto the desk</p>
  {{if .Observed}}
  <table>
    <tr><th>thread</th><th>participants</th><th>msgs</th><th>last activity</th></tr>
    {{range .Observed}}
    <tr>
      <td><a href="/thread/{{.ID}}">{{.ID}}</a></td>
      <td>{{.OpenedBy}}{{range .With}}, {{.}}{{end}}</td>
      <td>{{len .Msgs}}</td>
      <td>{{ago .Updated}} ago</td>
    </tr>
    {{end}}
  </table>
  {{else}}<div class="empty">no observed bus threads yet</div>{{end}}
{{end}}

{{if eq .Page "talk"}}
  <h1>Talk to an agent or mission</h1>
  {{if .TalkTarget}}
    <p class="meta">To {{if eq .TalkKind "agent"}}@{{end}}{{.TalkTarget}} · <a href="/talk">change target</a></p>
    <form class="stack" method="post" action="{{.TalkAction}}">
      <label>message</label>
      <textarea name="message" autofocus>{{index .F "message"}}</textarea>
      <label>title (optional)</label>
      <input type="text" name="title" value="{{index .F "title"}}" placeholder="Defaults to the first sentence, up to 80 characters">
      <details>
        <summary>Advanced</summary>
        <label>expects</label>
        <select name="expects">
          {{$e := index .F "expects"}}
          <option {{if eq $e "decide"}}selected{{end}}>decide</option>
          <option {{if eq $e "act"}}selected{{end}}>act</option>
          <option {{if eq $e "reply"}}selected{{end}}>reply</option>
          <option {{if eq $e "read"}}selected{{end}}>read</option>
        </select>
        <label>weight</label>
        <select name="weight">
          {{$w := index .F "weight"}}
          <option {{if eq $w "rule"}}selected{{end}}>rule</option>
          <option {{if eq $w "signoff"}}selected{{end}}>signoff</option>
          <option {{if eq $w "provision"}}selected{{end}}>provision</option>
          <option {{if eq $w "moment"}}selected{{end}}>moment</option>
          <option {{if eq $w "ack"}}selected{{end}}>ack</option>
        </select>
      </details>
      <button>Send</button>
    </form>
  {{else}}
    <p class="meta">Choose one. Roster links preselect the exact agent or mission.</p>
    <form class="stack" method="get" action="/talk">
      <label>agent seat</label><input type="text" name="agent" placeholder="for example: vile">
      <button>Talk to agent</button>
    </form>
    <form class="stack" method="get" action="/talk">
      <label>mission</label><input type="text" name="mission" placeholder="mission slug">
      <button class="quiet">Talk to mission</button>
    </form>
  {{end}}
{{end}}

{{if eq .Page "roster"}}
  <h1>Roster</h1>
  <p class="meta">{{if .ShowClosed}}<a href="/roster">active only</a>{{else}}<a href="/roster?all=1">show all (incl. unseated/inactive)</a>{{end}}</p>
  {{range .Groups}}
    <h2>{{.Dir}}{{with .Mission}} · <a href="/talk?mission={{.}}">talk to mission</a>{{end}}</h2>
    <table>
      <tr><th>name</th><th>tool</th><th>status</th><th>role</th><th>branch</th><th>unread</th><th></th></tr>
      {{range .Agents}}
      <tr>
        <td><strong>{{.Name}}</strong></td><td>{{.Tool}}</td>
        <td>{{.Status}}{{with .Detail}} <span class="meta">({{.}})</span>{{end}}</td>
        <td>{{.Role}}</td><td>{{.Branch}}</td><td>{{if .Unread}}{{.Unread}}{{end}}</td>
        <td><a href="/talk?agent={{.Name}}">talk to</a></td>
      </tr>
      {{end}}
    </table>
  {{else}}<div class="empty">no agents visible</div>{{end}}
{{end}}

<div class="footer">mc · bus cursor {{.Cursor}} · refresh for updates</div>
</main></body></html>{{end}}

{{define "card"}}
<div class="card">
  <a class="title" href="/thread/{{.ID}}">{{.Title}}</a>
  <div class="meta">
    <span class="badge {{.Expects}}">{{.Expects}}</span>
    <span class="badge">{{.Weight}}</span>
    {{if eq .Turn "owner"}}<span class="badge yourturn">your turn</span>{{end}}
    {{.OpenedBy}}{{with .With}} ⇄ {{range $i, $w := .}}{{if $i}}, {{end}}{{$w}}{{end}}{{end}}
    · {{len .Msgs}} msg · {{ago .Updated}} ago{{with .Home}} · {{.}}{{end}}
  </div>
</div>
{{end}}
`
