package main

const pageTpl = `
{{define "page"}}<!doctype html>
<html><head><meta charset="utf-8"><title>mc — {{.Page}}</title>
<meta name="viewport" content="width=device-width, initial-scale=1">
{{if .Auto}}<meta http-equiv="refresh" content="{{.Auto}}">{{end}}
<style>
:root{--fg:#1a1a1a;--dim:#777;--line:#e2e2e2;--accent:#0b57d0;--bg:#fafaf8;--card:#fff;--warn:#b3261e}
*{box-sizing:border-box}
body{font:15px/1.5 system-ui,sans-serif;color:var(--fg);background:var(--bg);margin:0}
body>header{position:fixed;inset:0 0 auto 0;z-index:5;min-height:3.4rem;display:flex;gap:1.2em;align-items:center;padding:.7em 1.2em;border-bottom:1px solid var(--line);background:var(--card)}
body>header .brand{font-weight:700}
body>header a{color:var(--fg);text-decoration:none}
body>header a.on{color:var(--accent);font-weight:600}
body>header .who{margin-left:auto;color:var(--dim);font-size:.85em}
.mission-filter{display:flex;gap:.4em;align-items:center;margin:0}.mission-filter label{font-size:.78em;color:var(--dim)}.mission-filter input{width:13em}.mission-filter button{margin:0;padding:.32em .7em}
.cockpit{display:grid;grid-template-columns:minmax(10rem,15vw) minmax(0,1fr) minmax(11rem,15vw);min-height:100vh;padding-top:3.4rem}
.rail,.object-panel{background:var(--card);padding:.8em;border-right:1px solid var(--line);min-width:0;overflow-wrap:anywhere}.object-panel{border-right:0;border-left:1px solid var(--line)}
.rail{max-height:calc(100vh - 3.4rem);position:sticky;top:3.4rem;overflow:auto;align-self:start}.rail h2,.object-panel h2{font-size:.72em;margin:1.1em 0 .35em}.rail-list{list-style:none;padding:0;margin:0}.rail-list a{display:block;color:var(--fg);text-decoration:none;padding:.24em .35em;border-radius:4px;white-space:nowrap;overflow:hidden;text-overflow:ellipsis}.rail-list a:hover,.rail-list a.on{background:#eef3fc;color:var(--accent)}.rail-agent{display:flex;gap:.35em;align-items:center;padding:.18em .35em}.status-dot{color:#27873f}.status-dot.idle{color:var(--dim)}
main.canvas{min-width:0;margin:0;padding:1.2em clamp(1em,2vw,2.4em)}main.canvas.conversation{width:100%}
.object-details>summary{display:none}.object-details:not([open])>:not(summary){display:block}.object-group{margin-bottom:1.2em}.object-group a{display:block;overflow-wrap:anywhere}
h2{font-size:1em;text-transform:uppercase;letter-spacing:.06em;color:var(--dim);margin:1.4em 0 .5em}
.card{background:var(--card);border:1px solid var(--line);border-radius:8px;padding:.8em 1em;margin:.5em 0}
.card a.title{font-weight:600;color:var(--fg);text-decoration:none}
.meta{color:var(--dim);font-size:.85em}
.badge{display:inline-block;font-size:.75em;padding:.05em .5em;border-radius:99px;border:1px solid var(--line);color:var(--dim);margin-right:.4em}
.badge.decide{border-color:#b3261e;color:#b3261e}
.badge.act{border-color:#7b4a00;color:#7b4a00}
.badge.reply{border-color:var(--accent);color:var(--accent)}
.badge.managed{border-color:var(--accent);color:var(--accent)}
.badge.observed{border-style:dashed;color:var(--dim)}
.badge.yourturn{background:var(--accent);border-color:var(--accent);color:#fff}
.err{background:#fdeceb;border:1px solid var(--warn);color:var(--warn);padding:.6em 1em;border-radius:8px;margin:.8em 0}
.warning{background:#fff7e0;border:1px solid #d7a600;color:#654b00;padding:.6em 1em;border-radius:8px;margin:.8em 0}
.ctx{position:sticky;top:4.4rem;z-index:2;background:#fffbe8;border:1px solid #e6d9a0;border-radius:8px;padding:.8em 1em;margin:.8em 0;max-height:12em;overflow:auto}
.msg{border-left:3px solid var(--line);padding:.3em .9em;margin:.7em 0}.msg-body{overflow-wrap:anywhere}.msg-body pre{font:13px/1.45 ui-monospace,SFMono-Regular,Consolas,monospace;white-space:pre-wrap;background:#f5f5f3;padding:.7em;border-radius:6px;overflow:auto}.message-fold{border:1px solid var(--line);border-radius:6px;padding:.5em}.day-separator{display:flex;align-items:center;gap:.7em;color:var(--dim);font-size:.75em;text-transform:uppercase;letter-spacing:.05em;margin:1.4em 0}.day-separator:before,.day-separator:after{content:"";height:1px;background:var(--line);flex:1}
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
.composer{position:sticky;bottom:0;z-index:3;background:linear-gradient(transparent,var(--bg) 18%);padding-top:1.2em}.composer-actions{display:flex;gap:.5em;align-self:stretch}.composer-actions button{white-space:nowrap}
details{margin:.8em 0}
summary{cursor:pointer;color:var(--dim)}
.empty{color:var(--dim);font-style:italic;padding:.6em 0}
table{border-collapse:collapse;width:100%}
td,th{text-align:left;padding:.3em .6em;border-bottom:1px solid var(--line);font-size:.9em}
th{color:var(--dim);font-weight:500}
.footer{color:var(--dim);font-size:.8em;margin:2em 0;text-align:center}
.document{background:var(--card);border:1px solid var(--line);border-radius:8px;padding:1em 1.2em;overflow-wrap:anywhere}
.document pre{font:13px/1.45 ui-monospace,SFMono-Regular,Consolas,monospace;white-space:pre;overflow:auto;background:#f5f5f3;padding:.8em;border-radius:6px}
.document code{font-family:ui-monospace,SFMono-Regular,Consolas,monospace}
.graph-controls{display:flex;gap:1em;align-items:center;flex-wrap:wrap;margin:.7em 0}.graph-controls a.on{font-weight:700;color:var(--accent)}
.graph-frame{background:var(--card);border:1px solid var(--line);border-radius:8px;overflow:auto}.mc-graph{display:block;width:100%;min-width:720px;height:auto}
.graph-matrix{overflow:auto}.graph-matrix caption{text-align:left;padding:.7em .6em;color:var(--dim)}.graph-matrix th{white-space:nowrap}.graph-matrix td{text-align:center}.graph-cell.managed{font-weight:700;color:var(--accent)}.graph-cell.observed{border-left:1px dashed var(--line);color:var(--dim)}.graph-cell.raise{color:var(--warn)}.graph-cell.cool2{opacity:.6}.graph-cell.cool3{opacity:.3}.graph-empty{color:var(--line)}.graph-dim{opacity:.15!important}
@media(max-width:899px){body>header{position:static;flex-wrap:wrap;gap:.7em}body>header>a:not(.brand-link){font-size:.85em}.mission-filter{order:3;width:100%}.mission-filter input{flex:1}.cockpit{display:block;min-height:0;padding-top:0}.rail{position:static;max-height:none;border-right:0;border-bottom:1px solid var(--line);overflow-x:auto}.rail h2{display:inline;margin-right:.4em}.rail-list{display:inline-flex;gap:.25em;margin-right:.8em}.rail-list a{max-width:12em}.rail-agents{display:none}main.canvas{padding:1em}.object-panel{border-left:0;border-top:1px solid var(--line)}.object-details>summary{display:list-item}.object-details:not([open])>:not(summary){display:none}.ctx{position:static;max-height:none}.composer{position:static}.rowform{display:block}.composer-actions{margin-top:.5em}.mc-graph{min-width:620px}}
</style></head><body>
<header>
  <a class="brand brand-link" href="{{stateURL "/" .MissionFilter}}">mc</a>
  <a href="{{stateURL "/" .MissionFilter}}" {{if eq .Page "inbox"}}class="on"{{end}}>inbox</a>
  <a href="{{stateURL "/threads" .MissionFilter}}" {{if eq .Page "threads"}}class="on"{{end}}>all threads</a>
  <a href="{{stateURL "/roster" .MissionFilter}}" {{if eq .Page "roster"}}class="on"{{end}}>roster</a>
  <a href="{{stateURL "/graph" .MissionFilter}}" {{if eq .Page "graph"}}class="on"{{end}}>graph</a>
  <a href="{{stateURL "/talk" .MissionFilter}}" {{if eq .Page "talk"}}class="on"{{end}}>talk to…</a>
  <form class="mission-filter" method="get" action="{{.CurrentPath}}">
    <label for="mission-filter">mission</label><input id="mission-filter" type="text" name="mission" value="{{.MissionFilter}}" placeholder="all">
    {{range .StateFields}}<input type="hidden" name="{{.Name}}" value="{{.Value}}">{{end}}<button class="quiet">Filter</button>
  </form>
  <span class="who">{{.User}} · seat @{{.Seat}}{{if .BusDir}} · LAB BUS{{end}}</span>
</header>
<div class="cockpit">
{{template "rail" .}}
<main class="canvas{{if .T}} conversation{{end}}">
{{if .Error}}<div class="err">{{.Error}}</div>{{end}}

{{if and (eq .Page "inbox") (not .T) (not .Graph)}}
  {{if .IngestWarn}}<div class="warning">{{.IngestWarn}}</div>{{end}}
  <h2>Missions</h2>
  {{if .MissionListErr}}<div class="warning">{{.MissionListErr}}</div>{{end}}
  {{range .Missions}}
    <div class="card"><a class="title" href="/mission/{{.Slug}}?mission={{.Slug}}">{{.Slug}}</a>
      {{if .OK}}<div class="meta">{{.Board.Total}} tasks{{range .Board.Counts}} · {{.Status}} {{.Count}}{{end}}</div>
      {{else}}<div class="meta"><span class="badge">degraded</span>{{.CardWarning}}</div>{{end}}
    </div>
  {{else}}{{if not .MissionListErr}}<div class="empty">no missions visible</div>{{end}}{{end}}
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

{{if eq .Page "mission"}}
  {{$m := .Mission}}
  <p class="meta"><a href="/">&larr; all missions</a></p>
  <h1 style="margin:.2em 0">{{$m.Slug}}</h1>
  <p><a href="/talk?mission={{$m.Slug}}">Talk to this mission</a> · <a href="/graph?mission={{$m.Slug}}">View agent graph</a></p>

  <h2>Goal / manifest</h2>
  {{if not $m.OK}}
    <div class="warning"><strong>Mission data unavailable{{with $m.Refusal}} ({{.}}){{end}}</strong>{{with $m.Reason}}<br>{{.}}{{end}}{{with $m.Remedy}}<br><span class="meta">{{.}}</span>{{end}}</div>
  {{else}}
    <div class="card"><strong>{{$m.Manifest.Mission}}</strong>
      <div class="meta">authority {{$m.Manifest.Authority}} · owner {{$m.Manifest.Owner}} · <span class="badge">{{$m.Manifest.Status}}</span> · created {{$m.Manifest.Created}}</div>
      <div class="meta">{{$m.MissionDir}}</div>
    </div>
  {{end}}

  <h2>Board</h2>
  {{range $m.Warnings}}<div class="warning">{{.}}</div>{{end}}
  {{if $m.OK}}
    <p>{{range $m.Board.Counts}}<span class="badge">{{.Status}} {{.Count}}</span>{{end}} <span class="meta">{{$m.Board.Total}} total</span></p>
    {{if $m.Board.Available}}
      <table><tr><th>task</th><th>status</th><th>labels</th></tr>
      {{range $m.Board.Tasks}}<tr id="{{lower .ID}}"><td><strong>{{.ID}}</strong> {{.Title}}</td><td><span class="badge">{{.Status}}</span></td><td>{{range .Labels}}<span class="badge">{{.}}</span>{{end}}</td></tr>{{else}}<tr><td colspan="3" class="empty">no tasks</td></tr>{{end}}
      </table>
    {{else}}<div class="warning">Board data is unavailable.</div>{{end}}
  {{else}}<div class="empty">board unavailable until mission status recovers</div>{{end}}

  <h2>Artifacts</h2>
  {{if .ArtifactWarn}}<div class="warning">{{.ArtifactWarn}}</div>{{end}}
  {{range .Artifacts}}<div class="card"><a class="title" href="{{.URL}}">{{.Path}}</a></div>
  {{else}}{{if not .ArtifactWarn}}<div class="empty">no readable artifacts</div>{{end}}{{end}}

  <h2>Threads</h2>
  {{range .MissionThreads}}<div class="card"><a class="title" href="/thread/{{.ID}}?mission={{$m.Slug}}">{{.Title}}</a>
    <div class="meta"><span class="badge {{.Grade}}">{{.Grade}}</span><span class="badge">{{.Status}}</span>{{.ID}} · {{len .Msgs}} msg · {{$stamp := stampTime .Updated}}<time datetime="{{$stamp.DateTime}}" title="{{$stamp.Absolute}}">{{$stamp.Relative}}</time></div></div>
  {{else}}<div class="empty">no threads homed here</div>{{end}}

  <h2>Seated agents</h2>
  {{if .MissionAgents}}<table><tr><th>name</th><th>tool</th><th>status</th><th>role</th><th></th></tr>
    {{range .MissionAgents}}<tr><td><strong>{{.Name}}</strong>{{if .Unmanaged}} <span class="badge">unmanaged</span>{{end}}</td><td>{{.Tool}}</td><td>{{.Status}}</td><td>{{.Role}}</td><td><a href="/talk?agent={{.Name}}">talk to</a></td></tr>{{end}}
  </table>{{else}}<div class="empty">no seated agents</div>{{end}}
{{end}}

{{if eq .Page "file"}}
  <p class="meta"><a href="/mission/{{.Mission.Slug}}">&larr; {{.Mission.Slug}} artifacts</a></p>
  <h1 style="margin:.2em 0">{{.FilePath}}</h1>
  {{if .FileRefusal}}
    <div class="warning"><strong>File unavailable ({{.FileRefusal}})</strong><br>{{.FileReason}}</div>
  {{else if .FileHTML}}<article class="document">{{.FileHTML}}</article>
  {{else}}<pre class="document">{{.FilePlain}}</pre>{{end}}
{{end}}

{{if .T}}{{template "conversation" .}}{{end}}

{{if and (eq .Page "inbox") .Graph}}{{with .Graph}}
  <h1 style="margin:.2em 0">Live agent map</h1>
  {{if .Warning}}<div class="warning">{{.Warning}}</div>{{end}}
  <p class="meta">as of {{.AsOf}} ({{.AsOfRelative}}) · read-only bus view · <a href="{{stateURL "/graph" $.MissionFilter}}">open full graph</a></p>
  <div class="graph-frame">{{.Content}}</div>
{{end}}{{end}}

{{if eq .Page "graph"}}{{with .Graph}}
  <h1 style="margin:.2em 0">Live agent map</h1>
  <div class="graph-controls">
    <span>window: {{range .WindowLinks}}<a href="{{.URL}}" {{if .On}}class="on"{{end}}>{{.Label}}</a> {{end}}</span>
    <span>view: {{range .ViewLinks}}<a href="{{.URL}}" {{if .On}}class="on"{{end}}>{{.Label}}</a> {{end}}</span>
    <span>auto: <a href="{{.AutoOffURL}}">off</a> <a href="{{.Auto10URL}}">10s</a></span>
    {{with .Mission}}<span>mission: <strong>{{.}}</strong></span>{{end}}
    {{with .Focus}}<span>focus: <strong>{{.}}</strong></span>{{end}}
  </div>
  {{if .Warning}}<div class="warning">{{.Warning}}</div>{{end}}
  <p class="meta">as of {{.AsOf}} ({{.AsOfRelative}}) · read-only bus view</p>
  <div class="graph-frame">{{.Content}}</div>
{{end}}{{end}}

{{if eq .Page "threads"}}
  <h1>All threads</h1>
  <p class="meta">bus threads mc tracks but does not manage — an explicit @{{.Seat}} raise promotes one onto the desk</p>
  {{if .Observed}}
  <table>
    <tr><th>thread</th><th>participants</th><th>msgs</th><th>last activity</th></tr>
    {{range .Observed}}
    <tr>
      <td><a href="/thread/{{.ID}}{{with .Home}}?mission={{.}}{{end}}">{{.ID}}</a></td>
      <td>{{.OpenedBy}}{{range .With}}, {{.}}{{end}}</td>
      <td>{{len .Msgs}}</td>
      <td>{{$stamp := stampTime .Updated}}<time datetime="{{$stamp.DateTime}}" title="{{$stamp.Absolute}}">{{$stamp.Relative}}</time></td>
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
    <h2>{{with .Mission}}<a href="/mission/{{.}}?mission={{.}}">mission: {{.}}</a> · <a href="/talk?mission={{.}}">talk to mission</a>{{else}}{{.Dir}}{{end}}</h2>
    {{if .Repos}}
      {{range .Repos}}
        <h3>repo: {{.Repo}}</h3>
        {{range .Branches}}
          <h4>branch: {{.Branch}}</h4>
          {{template "rosterAgents" .Agents}}
        {{end}}
      {{end}}
    {{else}}{{template "rosterAgents" .Agents}}{{end}}
  {{else}}<div class="empty">no agents visible</div>{{end}}
{{end}}

<div class="footer">mc · bus cursor {{.Cursor}} · refresh for updates</div>
</main>
{{template "objectPanel" .}}
</div></body></html>{{end}}

{{define "rail"}}<aside class="rail{{if .Inhabit}} thread-local{{end}}" aria-label="{{if .Inhabit}}Thread context{{else}}Cockpit navigation{{end}}">
  {{if .Inhabit}}{{with .T}}
    <h2>Pinned ask</h2><div class="meta">{{$.ContextHTML}}</div>
    <h2>Refs</h2><ul class="rail-list">{{range $.Object.Tasks}}<li><a href="{{.URL}}">{{.Label}}</a></li>{{end}}{{range $.Object.Files}}<li><a href="{{.URL}}">{{.Label}}</a></li>{{end}}{{if and (not $.Object.Tasks) (not $.Object.Files)}}<li class="empty">none</li>{{end}}</ul>
    <h2>Backlinks</h2><ul class="rail-list">{{range $.Object.Backlinks}}<li><a href="{{.URL}}">{{.Label}}</a></li>{{else}}<li class="empty">none</li>{{end}}</ul>
    <p class="meta"><a href="{{stateURL "/" $.MissionFilter}}">&larr; leave thread</a></p>
  {{end}}{{else}}
    <h2>Your turn</h2><ul class="rail-list">{{range .RailYourTurn}}<li id="rail-{{.ID}}"><a {{if and $.T (eq $.T.ID .ID)}}class="on"{{end}} href="/?{{if $.MissionFilter}}mission={{$.MissionFilter}}&amp;{{end}}peek={{.ID}}#rail-{{.ID}}"><span class="badge yourturn">{{.Expects}}</span>{{.Title}}</a></li>{{else}}<li class="empty">clear</li>{{end}}</ul>
    <h2>Waiting</h2><ul class="rail-list">{{range .RailWaiting}}<li id="rail-{{.ID}}"><a {{if and $.T (eq $.T.ID .ID)}}class="on"{{end}} href="/?{{if $.MissionFilter}}mission={{$.MissionFilter}}&amp;{{end}}peek={{.ID}}#rail-{{.ID}}">{{.Title}}</a></li>{{else}}<li class="empty">none</li>{{end}}</ul>
    <h2>Threads</h2><ul class="rail-list">{{range .RailThreads}}<li><a href="{{stateURL (printf "/thread/%s" .ID) $.MissionFilter}}">{{.Title}}</a></li>{{else}}<li class="empty">none</li>{{end}}</ul>
    <div class="rail-agents"><h2>Agents</h2>{{range .RailAgents}}<div class="rail-agent"><span class="status-dot{{if ne .Status "active"}} idle{{end}}">{{if eq .Status "active"}}●{{else}}○{{end}}</span><a href="/talk?agent={{.Name}}{{with $.MissionFilter}}&amp;mission={{.}}{{end}}">{{.Name}}</a>{{with .Tool}}<span class="meta">{{.}}</span>{{end}}</div>{{else}}<div class="empty">none visible</div>{{end}}</div>
    <p><a href="/?view=graph{{with .MissionFilter}}&amp;mission={{.}}{{end}}">⌗ graph</a></p>
  {{end}}
</aside>{{end}}

{{define "objectPanel"}}<aside class="object-panel" aria-label="About this view"><details class="object-details"><summary>About this thread</summary>
  {{if .T}}
    {{with .T.Home}}<div class="object-group"><h2>Mission</h2><a href="/mission/{{.}}?mission={{.}}">{{.}}</a></div>{{end}}
    <div class="object-group"><h2>Task</h2>{{range .Object.Tasks}}<a href="{{.URL}}">{{.Label}}</a>{{else}}<span class="empty">no task ref</span>{{end}}</div>
    <div class="object-group"><h2>Files / refs</h2>{{range .Object.Files}}<a href="{{.URL}}">{{.Label}}</a>{{else}}<span class="empty">no file refs</span>{{end}}</div>
    <div class="object-group"><h2>Backlinks</h2>{{range .Object.Backlinks}}<a href="{{.URL}}">{{.Label}}</a>{{else}}<span class="empty">none</span>{{end}}</div>
  {{else}}
    <h2>Desk</h2><p class="meta">Peek a thread to keep the queue in view, or inhabit it for a focused rail.</p>
  {{end}}
</details></aside>{{end}}

{{define "conversation"}}{{with .T}}
  <p class="meta">{{if $.Peek}}peek · <a href="{{stateURL (printf "/thread/%s" .ID) $.MissionFilter}}">inhabit thread</a>{{else}}inhabiting thread · <a href="{{stateURL "/" $.MissionFilter}}">cockpit</a>{{end}}</p>
  <h1 style="margin:.2em 0;white-space:nowrap;overflow:hidden;text-overflow:ellipsis" title="{{.Title}}">{{.Title}}</h1>
  <p class="meta">
    <span class="badge {{.Expects}}">{{.Expects}}</span><span class="badge">{{.Weight}}</span><span class="badge {{.Grade}}">{{.Grade}}</span>
    {{if eq .Grade "observed"}}{{else if eq .Status "closed"}}<span class="badge">closed</span>{{else if eq .Turn "owner"}}<span class="badge yourturn">your turn</span>{{else}}<span class="badge">waiting on {{.Turn}}</span>{{end}}
    opened by {{.OpenedBy}}{{with .With}} · with {{range $i, $w := .}}{{if $i}}, {{end}}{{$w}}{{end}}{{end}}{{with .Home}} · home {{.}}{{end}} · updated
    <time datetime="{{$.ThreadStamp.DateTime}}" title="{{$.ThreadStamp.Relative}}">{{$.ThreadStamp.Absolute}}</time> ({{$.ThreadStamp.Relative}}) · id {{.ID}}
  </p>
  {{with $.ParticipantState}}<p class="meta">{{.}}</p>{{end}}
  {{if eq .Grade "managed"}}<details><summary>Retitle</summary><form class="rowform" method="post" action="/thread/{{.ID}}/retitle{{with $.ReturnURL}}?return={{urlquery .}}{{end}}"><input type="text" name="title" value="{{.Title}}" aria-label="New title"><button class="quiet">Save title</button></form></details>{{end}}
  <div class="ctx">{{$.ContextHTML}}</div>
  {{if .Resolution}}<div class="card"><strong>Resolution:</strong> {{.Resolution}}</div>{{end}}
  {{range $.Messages}}
    {{if .DayBreak}}<div class="day-separator">{{.Day}}</div>{{end}}
    <article class="msg{{if hasHumanPrefix .From}} human{{end}}"><header class="from">{{.From}} <span class="meta">#{{.BusID}}{{with .Intent}} · {{.}}{{end}} · <time datetime="{{.Stamp.DateTime}}" title="{{.Stamp.Relative}}">{{.Stamp.Absolute}}</time> ({{.Stamp.Relative}})</span></header>
      <div class="msg-body">{{if .Folded}}<div class="message-preview">{{.Preview}}</div><details class="message-fold"><summary>Show remaining lines</summary><div class="message-rest">{{.Remainder}}</div></details>{{else}}{{.Preview}}{{end}}</div>
    </article>
  {{else}}<div class="empty">no messages linked yet</div>{{end}}
  {{if eq .Grade "managed"}}{{if eq .Status "open"}}
    <form class="rowform composer" method="post" action="/thread/{{.ID}}/reply{{with $.ReturnURL}}?return={{urlquery .}}{{end}}">
      <textarea name="text" placeholder="reply on this thread…"></textarea><div class="composer-actions"><button>Send</button><button class="quiet" formaction="/thread/{{.ID}}/close?cockpit=1{{with $.MissionFilter}}&amp;mission={{.}}{{end}}">Send &amp; close</button></div>
    </form>
  {{else}}<form method="post" action="/thread/{{.ID}}/reopen{{with $.ReturnURL}}?return={{urlquery .}}{{end}}"><button class="quiet">Reopen</button></form>{{end}}{{end}}
{{end}}{{end}}

{{define "rosterAgents"}}
<table>
  <tr><th>name</th><th>tool</th><th>status</th><th>role</th><th>branch</th><th>unread</th><th></th></tr>
  {{range .}}
  <tr>
    <td><strong>{{.Name}}</strong>{{if .Unmanaged}} <span class="badge">unmanaged</span>{{end}}</td><td>{{.Tool}}</td>
    <td>{{.Status}}{{with .Detail}} <span class="meta">({{.}})</span>{{end}}</td>
    <td>{{.Role}}</td><td>{{.Branch}}</td><td>{{if .Unread}}{{.Unread}}{{end}}</td>
    <td><a href="/talk?agent={{.Name}}">talk to</a></td>
  </tr>
  {{end}}
</table>
{{end}}

{{define "card"}}
<div class="card">
  <a class="title" href="/thread/{{.ID}}{{with .Home}}?mission={{.}}{{end}}">{{.Title}}</a>
  <div class="meta">
    <span class="badge {{.Expects}}">{{.Expects}}</span>
    <span class="badge">{{.Weight}}</span>
    {{if eq .Turn "owner"}}<span class="badge yourturn">your turn</span>{{end}}
    {{.OpenedBy}}{{with .With}} ⇄ {{range $i, $w := .}}{{if $i}}, {{end}}{{$w}}{{end}}{{end}}
    · {{len .Msgs}} msg · {{$stamp := stampTime .Updated}}<time datetime="{{$stamp.DateTime}}" title="{{$stamp.Absolute}}">{{$stamp.Relative}}</time>{{with .Home}} · {{.}}{{end}}
  </div>
</div>
{{end}}
`
