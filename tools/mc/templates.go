package main

// The skin consumes /tokens.css exclusively (TASK-45 vocabulary): raw hex
// or ad-hoc spacing in this file fails review, and the legacy inline vars
// (--accent/--dim/--warn …) are retired — one visual system only.
const pageTpl = `
{{define "page"}}<!doctype html>
<html><head><meta charset="utf-8"><title>mc — {{.Scope}}</title>
<meta name="viewport" content="width=device-width, initial-scale=1">
{{if .Auto}}<meta http-equiv="refresh" content="{{.Auto}}">{{end}}
<link rel="stylesheet" href="/tokens.css">
<style>
:root{color-scheme:light dark}
*{box-sizing:border-box}
body{font:var(--t-body);color:var(--c-fg);background:var(--c-bg);margin:0}
body>header{position:fixed;inset:0 0 auto 0;z-index:5;min-height:3.4rem;display:flex;gap:var(--s-4);align-items:center;padding:var(--s-2) var(--s-4);border-bottom:1px solid var(--c-line);background:var(--c-surface)}
body>header .brand{font-weight:700}
body>header .scope{color:var(--c-fg-dim);white-space:nowrap;overflow:hidden;text-overflow:ellipsis}
body>header a{color:var(--c-fg);text-decoration:none}
body>header a.on{color:var(--c-needs-you);font-weight:600}
body>header .who{margin-left:auto;color:var(--c-fg-dim);font:var(--t-small);text-align:right}
.cockpit{display:grid;grid-template-columns:minmax(10rem,15vw) minmax(0,1fr) minmax(11rem,15vw);min-height:100vh;padding-top:3.4rem}
.rail,.object-panel{background:var(--c-surface);padding:var(--s-2);border-right:1px solid var(--c-line);min-width:0;overflow-wrap:anywhere}.object-panel{border-right:0;border-left:1px solid var(--c-line)}
.rail{max-height:calc(100vh - 3.4rem);position:sticky;top:3.4rem;overflow:auto;align-self:start}.rail h2,.object-panel h2{font:var(--t-micro);text-transform:uppercase;letter-spacing:.06em;color:var(--c-fg-dim);margin:var(--s-4) 0 var(--s-1)}.rail-list{list-style:none;padding:0;margin:0}.rail-list a{display:block;color:var(--c-fg);text-decoration:none;padding:var(--s-1) var(--s-1);border-radius:var(--r-1);white-space:nowrap;overflow:hidden;text-overflow:ellipsis}.rail-list a:hover,.rail-list a.on{background:var(--c-needs-you-wash);color:var(--c-needs-you)}.rail-note{font:var(--t-small);padding:0 var(--s-1)}.rail-agent{display:flex;gap:var(--s-1);align-items:center;padding:var(--s-1)}.status-dot{color:var(--c-live)}.status-dot.idle{color:var(--c-fg-dim)}
main.canvas{min-width:0;margin:0;padding:var(--s-4) clamp(var(--s-4),2vw,var(--s-6))}main.canvas.conversation{width:100%}
.object-details>summary{display:none}.object-group{margin-bottom:var(--s-4)}.object-group a{display:block;overflow-wrap:anywhere}
h2{font:var(--t-small);text-transform:uppercase;letter-spacing:.06em;color:var(--c-fg-dim);margin:var(--s-5) 0 var(--s-2)}
h1{font:var(--t-display)}
.zone{margin:0 0 var(--s-5)}
.zone-h{font:var(--t-display);color:var(--c-fg);text-transform:uppercase;letter-spacing:.02em;margin:var(--s-4) 0 var(--s-3)}
.zone-h .zone-count{font:var(--t-small);color:var(--c-fg-dim);text-transform:none;letter-spacing:0}
.card{background:var(--c-surface);border:1px solid var(--c-line);border-radius:var(--r-2);padding:var(--s-3);margin:var(--s-2) 0}
.card a.title,.item a.title,.strip a.title{font:var(--t-title);color:var(--c-fg);text-decoration:none}
.meta{color:var(--c-fg-dim);font:var(--t-small)}
.mono{font:var(--t-mono)}
.prov{color:var(--c-fg-dim);font:var(--t-micro);font-family:var(--font-mono);margin:var(--s-2) 0}
.badge{display:inline-block;font:var(--t-micro);padding:0 var(--s-2);border-radius:var(--r-pill);border:1px solid var(--c-line);color:var(--c-fg-dim);margin-right:var(--s-1)}
.badge.decide,.badge.reply{border-color:var(--c-needs-you);color:var(--c-needs-you)}
.badge.act,.badge.read{border-color:var(--c-observed);color:var(--c-observed);border-style:dashed}
.badge.managed{border-color:var(--c-needs-you);color:var(--c-needs-you)}
.badge.observed{border-style:dashed;color:var(--c-observed)}
.badge.yourturn{background:var(--c-needs-you);border-color:var(--c-needs-you);color:var(--c-surface)}
.badge.blocked{background:var(--c-blocked-wash);border-color:var(--c-blocked);color:var(--c-blocked)}
.badge.answered{background:var(--c-done-wash);border-color:var(--c-done);color:var(--c-done)}
.blocked-ink{color:var(--c-blocked)}
.attested{border-left:3px solid var(--c-attested);background:var(--c-attested-wash);padding:var(--s-2) var(--s-3);margin:var(--s-2) 0}
.loud-gap{border:2px solid var(--c-warn);color:var(--c-warn);padding:var(--s-2);margin:var(--s-2) 0;font-weight:700}
.ask-option{border-left:3px solid var(--c-line);padding:var(--s-1) var(--s-3);margin:var(--s-2) 0}
.ask-actions{display:grid;grid-template-columns:repeat(auto-fit,minmax(15rem,1fr));gap:var(--s-4)}.ask-actions>details,.ask-actions>form{border:1px solid var(--c-line);border-radius:var(--r-2);padding:var(--s-2)}
.ask-trace{font:var(--t-mono);color:var(--c-fg-dim);margin:var(--s-1) 0}
.err{background:var(--c-blocked-wash);border:1px solid var(--c-blocked);color:var(--c-blocked);padding:var(--s-2) var(--s-3);border-radius:var(--r-2);margin:var(--s-2) 0}
.warning{background:var(--c-warn-wash);border:1px solid var(--c-warn);color:var(--c-warn);padding:var(--s-2) var(--s-3);border-radius:var(--r-2);margin:var(--s-2) 0}
.banner-degraded{background:var(--c-warn-wash);border:1px dashed var(--c-warn);color:var(--c-warn);padding:var(--s-2) var(--s-3);border-radius:var(--r-2);margin:var(--s-2) 0}
.group{border:1px solid var(--c-line);border-radius:var(--r-2);background:var(--c-surface);padding:var(--s-3);margin:var(--s-3) 0}
.group.has-blocked{border-color:var(--c-blocked)}
.group-h{font:var(--t-mono);color:var(--c-fg-dim);margin-bottom:var(--s-1)}
.group-ctx{margin-bottom:var(--s-2)}
.item{padding:var(--s-1) 0}
.degraded-item{color:var(--c-observed);font:var(--t-small);margin:var(--s-2) 0}
.degraded-item a{color:var(--c-observed)}
.loop-waiting{font:var(--t-small);color:var(--c-fg);padding:var(--s-1) 0}
.answer{border-left:3px solid var(--c-done);background:var(--c-done-wash);padding:var(--s-2) var(--s-3);margin:var(--s-2) 0;border-radius:var(--r-1)}
.strip{background:var(--c-surface);border:1px solid var(--c-line);border-radius:var(--r-2);padding:var(--s-3);margin:var(--s-3) 0}
.strip-h{display:flex;gap:var(--s-2);align-items:baseline;flex-wrap:wrap}
.strip-keys{margin-left:auto;font:var(--t-small);color:var(--c-fg-dim)}
.strip-line{margin:var(--s-1) 0}
.strip-line .k{display:inline-block;min-width:2.8em;color:var(--c-fg-dim);font:var(--t-small);text-transform:uppercase}
.annotation{font:var(--t-small);margin:var(--s-1) 0}
.annotation.made-for-you{color:var(--c-needs-you)}
.annotation.warn-ink{color:var(--c-warn)}
.health-line{color:var(--c-health);font:var(--t-small);margin:var(--s-1) 0}
.sys{color:var(--c-fg-dim);margin:var(--s-1) 0}
.ctx{position:sticky;top:4.4rem;z-index:2;background:var(--c-surface);border:1px solid var(--c-line);border-radius:var(--r-2);padding:var(--s-2) var(--s-3);margin:var(--s-2) 0;max-height:12em;overflow:auto}
.msg{border-left:3px solid var(--c-line);padding:var(--s-1) var(--s-3);margin:var(--s-2) 0}.msg-body{overflow-wrap:anywhere}.msg-body pre{font:var(--t-mono);white-space:pre-wrap;background:var(--c-bg);padding:var(--s-2);border-radius:var(--r-1);overflow:auto}.message-fold{border:1px solid var(--c-line);border-radius:var(--r-1);padding:var(--s-2)}.day-separator{display:flex;align-items:center;gap:var(--s-2);color:var(--c-fg-dim);font:var(--t-micro);text-transform:uppercase;letter-spacing:.05em;margin:var(--s-4) 0}.day-separator:before,.day-separator:after{content:"";height:1px;background:var(--c-line);flex:1}
.msg .from{font-weight:600;white-space:normal}
.msg.human{border-left-color:var(--c-needs-you)}
form.stack label{display:block;margin:var(--s-2) 0 var(--s-1);font:var(--t-small);color:var(--c-fg-dim)}
input[type=text],textarea,select{width:100%;padding:var(--s-1) var(--s-2);border:1px solid var(--c-line);border-radius:var(--r-1);font:inherit;background:var(--c-surface);color:var(--c-fg)}
textarea{min-height:6em}
button{font:inherit;padding:var(--s-1) var(--s-3);border:1px solid var(--c-needs-you);border-radius:var(--r-1);background:var(--c-needs-you);color:var(--c-surface);cursor:pointer;margin-top:var(--s-2)}
button.quiet{background:var(--c-surface);color:var(--c-fg);border-color:var(--c-line)}
.rowform{display:flex;gap:var(--s-2);align-items:flex-start;margin-top:var(--s-3)}
.rowform textarea{flex:1;min-height:3.4em}
.rowform button{margin-top:0}
.filterform{display:flex;gap:var(--s-1);align-items:center;margin:var(--s-2) 0}.filterform label{font:var(--t-small);color:var(--c-fg-dim)}.filterform input{width:13em}.filterform button{margin:0;padding:var(--s-1) var(--s-2)}
.composer{position:sticky;bottom:0;z-index:3;background:linear-gradient(transparent,var(--c-bg) 18%);padding-top:var(--s-4)}.composer-actions{display:flex;gap:var(--s-1);align-self:stretch}.composer-actions button{white-space:nowrap}
details{margin:var(--s-2) 0}
summary{cursor:pointer;color:var(--c-fg-dim)}
.empty{color:var(--c-fg-dim);font-style:italic;padding:var(--s-1) 0}
table{border-collapse:collapse;width:100%}
td,th{text-align:left;padding:var(--s-1) var(--s-2);border-bottom:1px solid var(--c-line);font:var(--t-small)}
th{color:var(--c-fg-dim);font-weight:500}
.footer{color:var(--c-fg-dim);font:var(--t-micro);font-family:var(--font-mono);margin:var(--s-6) 0;text-align:center}
.document{background:var(--c-surface);border:1px solid var(--c-line);border-radius:var(--r-2);padding:var(--s-3) var(--s-4);overflow-wrap:anywhere}
.document pre{font:var(--t-mono);white-space:pre;overflow:auto;background:var(--c-bg);padding:var(--s-2);border-radius:var(--r-1)}
.document code{font-family:var(--font-mono)}
.graph-controls{display:flex;gap:var(--s-4);align-items:center;flex-wrap:wrap;margin:var(--s-2) 0}.graph-controls a.on{font-weight:700;color:var(--c-needs-you)}
.graph-frame{background:var(--c-surface);border:1px solid var(--c-line);border-radius:var(--r-2);overflow:auto}.mc-graph{display:block;width:100%;min-width:720px;height:auto}
.graph-matrix{overflow:auto}.graph-matrix caption{text-align:left;padding:var(--s-2);color:var(--c-fg-dim)}.graph-matrix th{white-space:nowrap}.graph-matrix td{text-align:center}.graph-cell.managed{font-weight:700;color:var(--c-needs-you)}.graph-cell.observed{border-left:1px dashed var(--c-line);color:var(--c-fg-dim)}.graph-cell.raise{color:var(--c-warn)}.graph-cell.cool2{opacity:.6}.graph-cell.cool3{opacity:.3}.graph-empty{color:var(--c-line)}.graph-dim{opacity:.15!important}
@media(max-width:899px){body>header{position:static;flex-wrap:wrap;gap:var(--s-2)}body>header>a:not(.brand-link){font:var(--t-small)}.cockpit{display:block;min-height:0;padding-top:0}.rail{position:static;max-height:none;border-right:0;border-bottom:1px solid var(--c-line);overflow-x:auto}.rail h2{display:inline;margin-right:var(--s-1)}.rail-list{display:inline-flex;gap:var(--s-1);margin-right:var(--s-2)}.rail-list a{max-width:12em}.rail-note{display:inline}.rail-agents{display:none}main.canvas{padding:var(--s-3)}.object-panel{border-left:0;border-top:1px solid var(--c-line)}.object-details>summary{display:list-item}.ctx{position:static;max-height:none}.composer{position:static}.rowform{display:block}.composer-actions{margin-top:var(--s-2)}.strip-keys{margin-left:0;width:100%}button{min-height:2.75rem}.mc-graph{min-width:620px}}
</style><script src="/mc.js" defer></script></head><body data-view="{{.Page}}{{with .T}}:thread:{{.ID}}{{if $.Peek}}:peek{{end}}{{end}}{{if .Graph}}:graph{{end}}">
<header>
  <a class="brand brand-link" href="/">mc</a>
  <span class="scope">· {{.Scope}}</span>
  <a href="/asks" {{if or (eq .Page "asks") (eq .Page "mission-asks") (eq .Page "ask")}}class="on"{{end}}>asks</a>
  <a href="/threads" {{if eq .Page "threads"}}class="on"{{end}}>threads</a>
  <a href="/roster" {{if eq .Page "roster"}}class="on"{{end}}>roster</a>
  <a href="/talk" {{if eq .Page "talk"}}class="on"{{end}}>talk to…</a>
  <span class="who">seat: seated · {{.User}} @{{.Seat}}{{if .BusDir}} · LAB BUS{{end}}</span>
</header>
<div class="cockpit">
{{template "rail" .}}
<main class="canvas{{if .T}} conversation{{end}}">
{{if .Error}}<div class="err">{{.Error}}</div>{{end}}

{{if and (eq .Page "desk") (not .T) (not .Graph)}}<section data-live="desk">{{with .Desk}}
  <section class="zone" id="z1">
    <h2 class="zone-h">1 · Your open loops <span class="zone-count">({{.Answered}} answered, {{.WaitingCount}} waiting)</span></h2>
    {{range .Loops}}{{if .Answer}}{{template "loopCard" .}}{{end}}{{end}}
    {{range .Loops}}{{if not .Answer}}{{template "loopWaiting" .}}{{end}}{{end}}
    {{if not .Loops}}<div class="empty">no open loops</div>{{end}}
    <p class="prov">owner-outbound asks · board · mish status{{if .BoardStamp.DateTime}} · <time data-relative datetime="{{.BoardStamp.DateTime}}" title="{{.BoardStamp.Absolute}}">{{.BoardStamp.Relative}}</time>{{end}}</p>
  </section>
  <section class="zone" id="z2">
    <h2 class="zone-h">2 · Rulings backlog <span class="zone-count">({{.Zone2Count}} open · {{.Zone2Blocked}} blocked)</span></h2>
    {{range .Groups}}{{template "deskGroup" .}}{{else}}<div class="empty">no rulings wanted</div>{{end}}
    {{range .Degraded}}<div class="degraded-item">◌ degraded: targeted, no declared expects — {{.From}} · <time data-relative datetime="{{.Stamp.DateTime}}" title="{{.Stamp.Absolute}}">{{.Stamp.Relative}}</time> · <a href="{{.URL}}">{{.Title}}</a> (not counted; not your turn)</div>{{end}}
  </section>
  <section class="zone" id="z3">
    <h2 class="zone-h">3 · Missions <span class="zone-count">(by needs-me, then moved)</span></h2>
    {{if .BoardWarn}}<div class="warning">{{.BoardWarn}}</div>{{end}}
    {{range .Strips}}{{template "strip" .}}{{else}}{{if not .BoardWarn}}<div class="empty">no missions visible</div>{{end}}{{end}}
  </section>
{{end}}</section>{{end}}

{{if eq .Page "mission"}}
  {{$m := .Mission}}
  <p class="meta"><a href="/">&larr; desk</a></p>
  <h1>{{$m.Slug}}</h1>
  <p><a href="/mission/{{$m.Slug}}/asks">Asks board</a> · <a href="/mission/{{$m.Slug}}/review">Review debt</a> · <a href="/talk?mission={{$m.Slug}}">Talk to this mission</a> · <a href="/graph?mission={{$m.Slug}}">View agent graph</a></p>

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

  <h2>Asks</h2>
  {{if $m.Asks.Available}}
    <p>{{range $m.Asks.Counts}}<span class="badge">{{.State}} {{.Count}}</span>{{end}} <a href="/mission/{{$m.Slug}}/asks">open board</a></p>
    {{range $m.Asks.Entities}}{{if eq .State "open"}}{{template "askCard" .}}{{end}}{{else}}<div class="empty">no asks</div>{{end}}
    {{$fetched := stampTime $m.FetchedAt}}<p class="prov">board · mish status · <time data-relative datetime="{{$fetched.DateTime}}" title="{{$fetched.Absolute}}">{{$fetched.Relative}}</time></p>
  {{else}}<div class="empty">asks board not initialized</div>{{end}}

  <h2>Artifacts</h2>
  {{if .ArtifactWarn}}<div class="warning">{{.ArtifactWarn}}</div>{{end}}
  {{range .Artifacts}}<div class="card"><a class="title" href="{{.URL}}">{{.Path}}</a></div>
  {{else}}{{if not .ArtifactWarn}}<div class="empty">no readable artifacts</div>{{end}}{{end}}

  <h2>Threads</h2>
  {{range .MissionThreads}}<div class="card"><a class="title" href="/thread/{{.ID}}?mission={{$m.Slug}}">{{.Title}}</a>
    <div class="meta"><span class="badge {{.Grade}}">{{.Grade}}</span><span class="badge">{{.Status}}</span>{{.ID}} · {{len .Msgs}} msg · {{$stamp := stampTime .Updated}}<time data-relative datetime="{{$stamp.DateTime}}" title="{{$stamp.Absolute}}">{{$stamp.Relative}}</time></div></div>
  {{else}}<div class="empty">no threads homed here</div>{{end}}

  <h2>Seated agents</h2>
  {{if .MissionAgents}}<table><tr><th>name</th><th>tool</th><th>status</th><th>role</th><th></th></tr>
    {{range .MissionAgents}}<tr><td><strong>{{.Name}}</strong>{{if .Unmanaged}} <span class="badge">unmanaged</span>{{end}}{{with .MissionSource}} <span class="badge" title="mission membership source">mission: {{.}}</span>{{end}}</td><td>{{.Tool}}</td><td>{{.Status}}</td><td>{{.Role}}</td><td><a href="/talk?agent={{.Name}}">talk to</a></td></tr>{{end}}
  </table>{{else}}<div class="empty">no seated agents</div>{{end}}
{{end}}

{{if eq .Page "asks"}}
  <h1>Asks boards</h1>
  {{if .MissionListErr}}<div class="warning">{{.MissionListErr}}</div>{{end}}
  {{range .Missions}}<div class="card"><a class="title" href="/mission/{{.Slug}}/asks">{{.Slug}}</a>
    {{if .Asks.Available}}<div class="meta">{{.Asks.Total}} entities{{range .Asks.Counts}} · {{.State}} {{.Count}}{{end}}</div>{{else}}<div class="meta">board unavailable</div>{{end}}
  </div>{{else}}<div class="empty">no mission asks boards visible</div>{{end}}
  <p class="prov">board · mish status --all · current cached projection</p>
{{end}}

{{if eq .Page "review"}}{{$m := .Mission}}
  <p class="meta"><a href="/mission/{{$m.Slug}}">&larr; mission</a> · <a href="/mission/{{$m.Slug}}/asks">asks board</a></p>
  <h1>{{$m.Slug}} review debt</h1>
  <p class="meta">made-for-you decisions and review moments, grouped by their stored anchor</p>
  {{range .AskGroups}}<section id="{{.Anchor.Ref}}"><h2>{{.Anchor.Type}}:{{.Anchor.Ref}}</h2>{{range .Entities}}{{template "askCard" .}}{{end}}</section>{{else}}<div class="empty">no review debt</div>{{end}}
  {{$fetched := stampTime $m.FetchedAt}}<p class="prov">board · mish status · <time data-relative datetime="{{$fetched.DateTime}}" title="{{$fetched.Absolute}}">{{$fetched.Relative}}</time> · review is contextual</p>
{{end}}

{{if eq .Page "mission-asks"}}{{$m := .Mission}}
  <p class="meta"><a href="/asks">&larr; all asks boards</a> · <a href="/mission/{{$m.Slug}}">mission</a></p>
  <h1>{{$m.Slug}} asks</h1>
  {{range $m.Warnings}}<div class="warning">{{.}}</div>{{end}}
  {{if $m.Asks.Available}}
    <p>{{range $m.Asks.Counts}}<span class="badge">{{.State}} {{.Count}}</span>{{end}} <span class="meta">{{$m.Asks.Total}} total</span></p>
    {{range .AskGroups}}<section id="{{.Anchor.Ref}}"><h2>{{.Anchor.Type}}:{{.Anchor.Ref}}</h2>{{range .Entities}}{{template "askCard" .}}{{end}}</section>{{else}}<div class="empty">no asks</div>{{end}}
    {{$fetched := stampTime $m.FetchedAt}}<p class="prov">board · mish status · <time data-relative datetime="{{$fetched.DateTime}}" title="{{$fetched.Absolute}}">{{$fetched.Relative}}</time></p>
  {{else}}<div class="empty">asks board not initialized</div>{{end}}
{{end}}

{{if eq .Page "ask"}}{{$a := .Ask}}
  <p class="meta"><a href="/mission/{{.AskMission.Slug}}/asks">&larr; {{.AskMission.Slug}} asks</a></p>
  {{if .AskError}}<div class="warning">{{$a.ID}} unavailable: {{.AskError}}</div>{{else}}
  <h1>{{$a.Framing.Question}}</h1>
  <p class="meta"><span class="badge {{$a.Expects}}">{{$a.Expects}}</span><span class="badge">{{$a.Kind}} / {{$a.State}}{{with $a.Outcome}} / {{.}}{{end}}</span> {{$a.Asker}} ⇄ {{$a.AddressedTo}} · updated {{$a.UpdatedAt}} · id {{$a.ID}}</p>
  {{if not $a.Framing.Question}}<div class="loud-gap">MALFORMED ASK — question missing</div>{{end}}
  {{if not $a.Framing.Context}}<div class="loud-gap">MALFORMED ASK — context missing</div>{{else}}<div class="ctx">{{$a.Framing.Context}}</div>{{end}}
  {{with $a.Blocking}}<div class="attested"><span class="badge">attested</span> {{.Actor}} · {{.At}} · “{{.Fact}}”</div>{{end}}
  {{if eq $a.Expects "decide"}}
    <h2>Decision</h2>
    {{range $a.Framing.SubDecisions}}<span class="badge">{{.}}</span>{{else}}<div class="loud-gap">MALFORMED ASK — sub-decisions missing</div>{{end}}
    {{range $a.Framing.Options}}<div class="ask-option"><strong>{{.ID}} — {{.Label}}</strong><div class="meta">cost: {{.Cost}} · risk: {{.Risk}} · blast radius: {{.BlastRadius}}</div>{{if or (not .ID) (not .Label) (not .Cost) (not .Risk) (not .BlastRadius)}}<div class="loud-gap">MALFORMED OPTION — required decision detail missing</div>{{end}}</div>{{else}}<div class="loud-gap">MALFORMED ASK — options missing</div>{{end}}
    {{with $a.Framing.Recommendation}}<p><strong>Recommendation: {{.Choice}}</strong> — {{.Reason}}</p>{{else}}<div class="loud-gap">MALFORMED ASK — recommendation missing</div>{{end}}
    {{if $a.Framing.DoNothing}}<p><strong>Do nothing:</strong> {{$a.Framing.DoNothing}}</p>{{else}}<div class="loud-gap">MALFORMED ASK — do-nothing consequence missing</div>{{end}}
  {{end}}
  <h2>Discussion</h2>
  <p class="meta">dyad: {{$a.Asker}} + {{$a.AddressedTo}} · linked threads are relations, not containers</p>
  {{range $a.Replies}}<article class="msg"><header class="from">{{.Actor}} <span class="meta">{{.At}} · {{.ID}}</span></header><div class="msg-body">{{.Prose}}</div></article>{{else}}<div class="empty">no replies</div>{{end}}
  <h2>Ruling trail</h2>
  {{range $a.Rulings}}<div class="card"><strong>{{with .Choice}}choice {{.}}{{else}}prose ruling{{end}}</strong> · {{.Actor}} · {{.At}}<p>{{.Prose}}</p><div class="meta">question as presented: {{.Question}}</div>{{range .OptionsAsPresented}}<div class="meta">{{.ID}} — {{.Label}}</div>{{end}}</div>{{else}}<div class="empty">not settled</div>{{end}}
  <h2>SINCE</h2>
  {{if $a.Rulings}}<p class="meta">movement on linked anchors after settlement</p>{{range $a.Links}}<div class="card">{{.Type}}:{{.Ref}}<div class="meta">relation · mish asks view</div></div>{{else}}<div class="empty">no linked anchors to inspect</div>{{end}}{{else}}<div class="empty">available after settlement</div>{{end}}
  <h2>Co-custodian traces</h2>
  {{range $a.Traces}}<div class="ask-trace">{{.At}} · {{.Actor}} · {{.Action}}{{with .Outcome}} · {{.}}{{end}}{{with .Reason}} · {{.}}{{end}}{{with .Member}} · member {{.}}{{end}}{{with .Citation}} · citation {{.Type}}:{{.Ref}}{{end}}</div>{{else}}<div class="empty">no traces</div>{{end}}
  <h2>Relations</h2><p class="meta">anchor {{$a.Anchor.Type}}:{{$a.Anchor.Ref}}</p>{{range $a.Links}}<span class="badge">{{.Type}}:{{.Ref}}</span>{{else}}<span class="empty">no related items</span>{{end}}
  {{range $a.Warnings}}<div class="warning">{{.}}</div>{{end}}
  {{if eq $a.State "open"}}<h2>Actions</h2><div class="ask-actions">
    <form class="stack" method="post" action="/ask/{{$a.ID}}/reply"><input type="hidden" name="if_updated_at" value="{{$a.UpdatedAt}}"><label>Reply</label><textarea name="prose">{{index $.AskDraft "prose"}}</textarea><button>Reply</button></form>
    {{if $.ConfirmSettle}}<form class="stack" method="post" action="/ask/{{$a.ID}}/settle"><input type="hidden" name="if_updated_at" value="{{$a.UpdatedAt}}"><input type="hidden" name="confirmed" value="true"><input type="hidden" name="choice" value="{{index $.AskDraft "choice"}}"><input type="hidden" name="prose" value="{{index $.AskDraft "prose"}}"><strong>Confirm settlement</strong><p>Choice: {{index $.AskDraft "choice"}}<br>Prose: {{index $.AskDraft "prose"}}</p><button>Settle this ask</button> <a href="/ask/{{$a.ID}}">cancel</a></form>{{else}}<form class="stack" method="get" action="/ask/{{$a.ID}}"><input type="hidden" name="confirm" value="settle"><label>Choice (optional)</label><select name="choice"><option value="">prose only</option>{{range $a.Framing.Options}}<option value="{{.ID}}">{{.ID}} — {{.Label}}</option>{{end}}</select><label>Ruling prose</label><textarea name="prose"></textarea><button>Review settlement</button></form>{{end}}
    <details><summary>Close without settlement</summary><form class="stack" method="post" action="/ask/{{$a.ID}}/close"><input type="hidden" name="if_updated_at" value="{{$a.UpdatedAt}}"><label>Outcome</label><select name="outcome"><option>no-action</option><option>superseded</option></select><label>Reason (optional)</label><textarea name="reason">{{index $.AskDraft "reason"}}</textarea><button>Close</button></form></details>
    <details><summary>Link related item</summary><form class="stack" method="post" action="/ask/{{$a.ID}}/link"><input type="hidden" name="if_updated_at" value="{{$a.UpdatedAt}}"><label>Type</label><select name="link_type"><option>task</option><option>phase</option><option>milestone</option><option>artifact</option><option>thread</option><option>mission</option><option>entity</option></select><label>Reference</label><input type="text" name="link_ref" value="{{index $.AskDraft "link_ref"}}"><label><input type="checkbox" name="set_anchor" value="true"> also adopt as anchor</label><button>Link</button></form></details>
    {{if $.CanWiden}}<details><summary>Widen membership</summary><form class="stack" method="post" action="/ask/{{$a.ID}}/widen"><input type="hidden" name="if_updated_at" value="{{$a.UpdatedAt}}"><label>Member actor string</label><input type="text" name="member" value="{{index $.AskDraft "member"}}"><button>Widen</button></form></details>{{end}}
  </div>{{end}}
  <p class="prov">board · mish asks view · framing immutable · close never stops traffic</p>
  {{end}}
{{end}}

{{if eq .Page "file"}}
  <p class="meta"><a href="/mission/{{.Mission.Slug}}">&larr; {{.Mission.Slug}} artifacts</a></p>
  <h1>{{.FilePath}}</h1>
  {{if .FileRefusal}}
    <div class="warning"><strong>File unavailable ({{.FileRefusal}})</strong><br>{{.FileReason}}</div>
  {{else if .FileHTML}}<article class="document">{{.FileHTML}}</article>
  {{else}}<pre class="document">{{.FilePlain}}</pre>{{end}}
{{end}}

{{if .T}}{{template "conversation" .}}{{end}}

{{if and (eq .Page "desk") .Graph}}<section data-live="graph">{{with .Graph}}
  <h1>Live agent map</h1>
  {{if .Warning}}<div class="warning">{{.Warning}}</div>{{end}}
  <p class="meta">as of {{.AsOf}} ({{.AsOfRelative}}) · read-only bus view · <a href="/graph">open full graph</a></p>
  <div class="graph-frame">{{.Content}}</div>
{{end}}</section>{{end}}

{{if eq .Page "graph"}}{{with .Graph}}
  <h1>Live agent map</h1>
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
  <p class="meta">every bus thread mc tracks, managed and observed — browsable when you go looking, never pushed</p>
  <form class="filterform" method="get" action="/threads">
    <label for="threads-mission">mission</label><input id="threads-mission" type="text" name="mission" value="{{.MissionFilter}}" placeholder="all"><button class="quiet">Filter</button>
  </form>
  {{if .Observed}}
  <table>
    <tr><th>thread</th><th>grade</th><th>status</th><th>participants</th><th>msgs</th><th>last activity</th></tr>
    {{range .Observed}}
    <tr>
      <td><a href="/thread/{{.ID}}{{with .Home}}?mission={{.}}{{end}}">{{.Title}}</a></td>
      <td><span class="badge {{.Grade}}">{{.Grade}}</span></td>
      <td>{{.Status}}</td>
      <td>{{.OpenedBy}}{{range .With}}, {{.}}{{end}}</td>
      <td>{{len .Msgs}}</td>
      <td>{{$stamp := stampTime .Updated}}<time data-relative datetime="{{$stamp.DateTime}}" title="{{$stamp.Absolute}}">{{$stamp.Relative}}</time></td>
    </tr>
    {{end}}
  </table>
  {{else}}<div class="empty">no tracked bus threads yet</div>{{end}}
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
  <form class="filterform" method="get" action="/roster">
    <label for="roster-mission">mission</label><input id="roster-mission" type="text" name="mission" value="{{.MissionFilter}}" placeholder="all">{{range .StateFields}}<input type="hidden" name="{{.Name}}" value="{{.Value}}">{{end}}<button class="quiet">Filter</button>
  </form>
  {{range .Groups}}
    <h2>{{with .Mission}}<a href="/mission/{{.}}">mission: {{.}}</a> · <a href="/talk?mission={{.}}">talk to mission</a>{{else}}{{.Dir}}{{end}}</h2>
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

{{define "loopCard"}}
<div class="card loop" id="loop-{{.Ask.ID}}">
  <div class="meta">you asked {{.Ask.AddressedTo}} · {{.Mission}}{{if .Ask.Anchor.Ref}} › {{.Ask.Anchor.Type}} {{.Ask.Anchor.Ref}}{{end}}</div>
  <a class="title" href="/ask/{{.Ask.ID}}">{{if .Ask.Framing.Question}}{{.Ask.Framing.Question}}{{else}}MALFORMED ASK — question missing{{end}}</a>
  <span class="meta"><time data-relative datetime="{{.Asked.DateTime}}" title="{{.Asked.Absolute}}">{{.Asked.Relative}}</time></span>
  {{with .Answer}}<div class="answer"><span class="badge answered">answer</span> <strong>{{.Actor}}</strong> · <time data-relative datetime="{{$.AnswerStamp.DateTime}}" title="{{$.AnswerStamp.Absolute}}">{{$.AnswerStamp.Relative}}</time>
    <div class="msg-body">{{.Prose}}</div></div>{{end}}
  <form class="rowform" method="post" action="/ask/{{.Ask.ID}}/reply?return=%2F%23z1">
    <input type="hidden" name="if_updated_at" value="{{.Ask.UpdatedAt}}">
    <textarea name="prose" placeholder="reply in place…" aria-label="Reply to {{.Ask.AddressedTo}}"></textarea>
    <div class="composer-actions"><button>send</button><button class="quiet" formmethod="get" formaction="/ask/{{.Ask.ID}}" name="confirm" value="settle">send &amp; settle…</button></div>
  </form>
</div>
{{end}}

{{define "loopWaiting"}}
<div class="loop-waiting" id="loop-{{.Ask.ID}}">waiting on {{.Ask.AddressedTo}} · <a href="/ask/{{.Ask.ID}}">{{if .Ask.Framing.Question}}{{.Ask.Framing.Question}}{{else}}MALFORMED ASK — question missing{{end}}</a> · asked <time data-relative datetime="{{.Asked.DateTime}}" title="{{.Asked.Absolute}}">{{.Asked.Relative}}</time></div>
{{end}}

{{define "deskGroup"}}
<section class="group{{if .HasBlocked}} has-blocked{{end}}" id="{{.FragmentID}}">
  <div class="group-h">anchor: {{.AnchorLabel}} — {{len .Items}} item{{if ne (len .Items) 1}}s{{end}}</div>
  {{with .Context}}<div class="group-ctx meta">ctx: {{.}}</div>{{end}}
  {{range .Items}}
  <div class="item">
    <span class="badge {{.Expects}}">{{.Expects}}</span>{{with .Blocked}}<span class="badge blocked" title="attested — {{.Actor}} · {{.At}}">claims blocked: {{.Fact}}</span>{{end}}
    <a class="title" href="{{.URL}}">{{.Title}}</a>
    <span class="meta">{{.From}} · <time data-relative datetime="{{.Stamp.DateTime}}" title="{{.Stamp.Absolute}}">{{.Stamp.Relative}}</time></span>
  </div>
  {{end}}
  {{range .Traces}}<div class="ask-trace">{{.Action}} by {{.Actor}} · {{.At}}{{with .Citation}} · cites: {{.Type}}:{{.Ref}}{{end}} · <a href="/ask/{{.AskID}}">trace</a></div>{{end}}
</section>
{{end}}

{{define "strip"}}
<article class="strip" id="strip-{{.Slug}}">
  <header class="strip-h"><a class="title" href="/mission/{{.Slug}}">{{.Slug}}</a>
    <span class="strip-keys">{{if .NeedsYou}}needs you: {{.NeedsYou}}{{end}}{{if .Blocked}} · <span class="blocked-ink">● blocked</span>{{end}}</span>
  </header>
  {{if not .OK}}<div class="warning">board unavailable — {{.Warning}}</div>
  {{else if .NoNarrative}}<div class="banner-degraded">no narrative layer — board is task-grain only ⚠</div>
  {{else}}
  <div class="strip-line mono">phase: {{.Phase}} ({{.MilestonesDone}} of {{.MilestonesTotal}} milestones done)</div>
  {{if .Done}}<div class="strip-line"><span class="k">done</span> {{join .Done " · "}}</div>{{end}}
  {{if .Now}}<div class="strip-line"><span class="k">now</span> {{.Now}} ({{.NowDone}}/{{.NowTotal}} done){{range .NowTasks}} · {{.Title}} — {{.Status}}{{end}}</div>{{end}}
  {{if .Next}}<div class="strip-line"><span class="k">next</span> {{join .Next " · "}}</div>{{end}}
  {{end}}
  {{if .ReviewDebt}}<div class="annotation made-for-you">⚑ made for you ({{.ReviewDebt}}){{if .MadeForYou}}: {{join .MadeForYou " · "}}{{end}} <a href="/mission/{{.Slug}}/review">[review →]</a></div>{{end}}
  {{with .Spin}}<div class="annotation warn-ink">⚠ spinning: {{.Events}} bus events, 0 board moves since <time datetime="{{.Since.DateTime}}" title="{{.Since.Relative}}">{{.Since.Absolute}}</time></div>{{end}}
  {{if .OK}}<div class="prov">crew-authored (board milestones) · board · mish status · <time data-relative datetime="{{.Fetched.DateTime}}" title="{{.Fetched.Absolute}}">{{.Fetched.Relative}}</time></div>{{end}}
</article>
{{end}}

{{define "rail"}}<aside class="rail{{if .Inhabit}} thread-local{{end}}" data-live="rail" aria-label="{{if .Inhabit}}Thread context{{else}}Cockpit navigation{{end}}">
  {{if .Inhabit}}{{with .T}}
    <h2>Pinned ask</h2><div class="meta">{{$.ContextHTML}}</div>
    <h2>Refs</h2><ul class="rail-list">{{range $.Object.Tasks}}<li><a href="{{.URL}}">{{.Label}}</a></li>{{end}}{{range $.Object.Files}}<li><a href="{{.URL}}">{{.Label}}</a></li>{{end}}{{if and (not $.Object.Tasks) (not $.Object.Files)}}<li class="empty">none</li>{{end}}</ul>
    <h2>Backlinks</h2><ul class="rail-list">{{range $.Object.Backlinks}}<li><a href="{{.URL}}">{{.Label}}</a></li>{{else}}<li class="empty">none</li>{{end}}</ul>
    <p class="meta"><a href="/">&larr; leave thread</a></p>
  {{end}}{{else}}
    <h2>Your turn{{if .RailItems}} · {{len .RailItems}}{{end}}</h2>
    {{if .RailBlocked}}<div class="rail-note blocked-ink">● blocked {{.RailBlocked}}</div>{{end}}
    <ul class="rail-list">{{range .RailItems}}<li{{with .ThreadID}} id="rail-{{.}}"{{end}}><a {{if and $.T .Thread (eq $.T.ID .Thread.ID)}}class="on" {{end}}href="{{.URL}}"><span class="badge yourturn">{{.Expects}}</span>{{.Title}}</a></li>{{else}}<li class="empty">clear</li>{{end}}</ul>
    <h2>Open loops</h2>
    <ul class="rail-list"><li><a href="/#z1">{{.RailLoops}} open loop{{if ne .RailLoops 1}}s{{end}}</a></li></ul>
    <h2>Missions</h2>
    <ul class="rail-list">{{range .RailMissions}}<li><a href="/mission/{{.Slug}}">{{.Slug}}{{if or .Spin (not .OK)}} ⚠{{end}}</a></li>{{else}}<li class="empty">none</li>{{end}}</ul>
    <div class="rail-agents"><h2>Agents</h2>{{range .RailAgents}}<div class="rail-agent"><span class="status-dot{{if ne .Status "active"}} idle{{end}}">{{if eq .Status "active"}}●{{else}}○{{end}}</span><a href="/talk?agent={{.Name}}">{{.Name}}</a>{{with .Tool}}<span class="meta">{{.}}</span>{{end}}</div>{{else}}<div class="empty">none visible</div>{{end}}</div>
    <p><a href="/?view=graph">⌗ graph</a></p>
  {{end}}
</aside>{{end}}

{{define "objectPanel"}}<aside class="object-panel" data-live="object-panel" aria-label="{{if .T}}About this thread{{else}}System{{end}}"><details class="object-details" open><summary>{{if .T}}About this thread{{else}}System{{end}}</summary>
  {{if .T}}
    {{with .T.Home}}<div class="object-group"><h2>Mission</h2><a href="/mission/{{.}}">{{.}}</a></div>{{end}}
    <div class="object-group"><h2>Task</h2>{{range .Object.Tasks}}<a href="{{.URL}}">{{.Label}}</a>{{else}}<span class="empty">no task ref</span>{{end}}</div>
    <div class="object-group"><h2>Files / refs</h2>{{range .Object.Files}}<a href="{{.URL}}">{{.Label}}</a>{{else}}<span class="empty">no file refs</span>{{end}}</div>
    <div class="object-group"><h2>Backlinks</h2>{{range .Object.Backlinks}}<a href="{{.URL}}">{{.Label}}</a>{{else}}<span class="empty">none</span>{{end}}</div>
  {{else}}
    <h2>Seat</h2>
    <div class="sys mono">seat: seated · @{{.Seat}}</div>
    <div class="sys mono">{{.User}}</div>
    <h2>Health</h2>
    {{if .IngestWarn}}<div class="health-line">ingest: {{.IngestWarn}} ⚠</div>{{end}}
    {{with .Desk}}
      {{if .UnanchoredAsks}}<div class="health-line"><a href="/asks">unanchored asks: {{.UnanchoredAsks}} ⚠</a></div>{{end}}
      {{range .DegradedBoards}}<div class="health-line">board degraded: {{.}} ⚠</div>{{end}}
      {{if and (not .UnanchoredAsks) (not .DegradedBoards) (not $.IngestWarn)}}<div class="empty">no health warnings</div>{{end}}
      <p class="prov">board · mish status --all{{if .BoardStamp.DateTime}} · <time data-relative datetime="{{.BoardStamp.DateTime}}" title="{{.BoardStamp.Absolute}}">{{.BoardStamp.Relative}}</time>{{end}}</p>
    {{end}}
  {{end}}
</details></aside>{{end}}

{{define "conversation"}}{{with .T}}
  <section data-live="thread-head-{{.ID}}">
  <p class="meta">{{if $.Peek}}peek · <a href="/thread/{{.ID}}">inhabit thread</a>{{else}}inhabiting thread · <a href="/">desk</a>{{end}}</p>
  <h1 style="margin:0;white-space:nowrap;overflow:hidden;text-overflow:ellipsis" title="{{.Title}}">{{.Title}}</h1>
  <p class="meta">
    <span class="badge {{.Expects}}">{{.Expects}}</span><span class="badge">{{.Weight}}</span><span class="badge {{.Grade}}">{{.Grade}}</span>
    {{if eq .Grade "observed"}}{{else if eq .Status "closed"}}<span class="badge">closed</span>{{else if eq .Turn "owner"}}<span class="badge yourturn">your turn</span>{{else}}<span class="badge">waiting on {{.Turn}}</span>{{end}}
    opened by {{.OpenedBy}}{{with .With}} · with {{range $i, $w := .}}{{if $i}}, {{end}}{{$w}}{{end}}{{end}}{{with .Home}} · home {{.}}{{end}} · updated
    <time datetime="{{$.ThreadStamp.DateTime}}" title="{{$.ThreadStamp.Relative}}">{{$.ThreadStamp.Absolute}}</time> (<time data-relative datetime="{{$.ThreadStamp.DateTime}}">{{$.ThreadStamp.Relative}}</time>) · id {{.ID}}
  </p>
  {{with $.ParticipantState}}<p class="meta">{{.}}</p>{{end}}
  {{if eq .Grade "managed"}}<details><summary>Retitle</summary><form class="rowform" method="post" action="/thread/{{.ID}}/retitle{{with $.ReturnURL}}?return={{urlquery .}}{{end}}"><input type="text" name="title" value="{{.Title}}" aria-label="New title"><button class="quiet">Save title</button></form></details>{{end}}
  <div class="ctx">{{$.ContextHTML}}</div>
  {{if .Resolution}}<div class="card"><strong>Resolution:</strong> {{.Resolution}}</div>{{end}}
  </section>
  <section data-live="thread-tail-{{.ID}}">
  {{range $.Messages}}
    {{if .DayBreak}}<div class="day-separator">{{.Day}}</div>{{end}}
    <article class="msg{{if hasHumanPrefix .From}} human{{end}}"><header class="from">{{.From}} <span class="meta">#{{.BusID}}{{with .Intent}} · {{.}}{{end}} · <time datetime="{{.Stamp.DateTime}}" title="{{.Stamp.Relative}}">{{.Stamp.Absolute}}</time> (<time data-relative datetime="{{.Stamp.DateTime}}">{{.Stamp.Relative}}</time>)</span></header>
      <div class="msg-body">{{if .Folded}}<div class="message-preview">{{.Preview}}</div><details class="message-fold"><summary>Show remaining lines</summary><div class="message-rest">{{.Remainder}}</div></details>{{else}}{{.Preview}}{{end}}</div>
    </article>
  {{else}}<div class="empty">no messages linked yet</div>{{end}}
  </section>
  <div data-live="thread-controls-{{.ID}}">{{if eq .Grade "managed"}}{{if eq .Status "open"}}
    <form class="rowform composer" method="post" action="/thread/{{.ID}}/reply{{with $.ReturnURL}}?return={{urlquery .}}{{end}}">
      <textarea name="text" placeholder="reply on this thread…"></textarea><div class="composer-actions"><button>Send</button><button class="quiet" formaction="/thread/{{.ID}}/close?cockpit=1">Send &amp; close</button></div>
    </form>
  {{else}}<form method="post" action="/thread/{{.ID}}/reopen{{with $.ReturnURL}}?return={{urlquery .}}{{end}}"><button class="quiet">Reopen</button></form>{{end}}{{end}}</div>
{{end}}{{end}}

{{define "rosterAgents"}}
<table>
  <tr><th>name</th><th>tool</th><th>status</th><th>role</th><th>branch</th><th>unread</th><th></th></tr>
  {{range .}}
  <tr>
    <td><strong>{{.Name}}</strong>{{if .Unmanaged}} <span class="badge">unmanaged</span>{{end}}{{with .MissionSource}} <span class="badge" title="mission membership source">mission: {{.}}</span>{{end}}</td><td>{{.Tool}}</td>
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
    · {{len .Msgs}} msg · {{$stamp := stampTime .Updated}}<time data-relative datetime="{{$stamp.DateTime}}" title="{{$stamp.Absolute}}">{{$stamp.Relative}}</time>{{with .Home}} · {{.}}{{end}}
  </div>
</div>
{{end}}

{{define "askCard"}}<div class="card">
  <a class="title" href="/ask/{{.ID}}">{{if .Framing.Question}}{{.Framing.Question}}{{else}}MALFORMED ASK — question missing{{end}}</a>
  <div class="meta"><span class="badge {{.Expects}}">{{.Expects}}</span><span class="badge">{{.Kind}} / {{.State}}{{with .Outcome}} / {{.}}{{end}}</span> {{.Asker}} ⇄ {{.AddressedTo}} · anchor {{.Anchor.Type}}:{{.Anchor.Ref}} · {{len .Replies}} replies</div>
  {{with .Blocking}}<div class="attested"><span class="badge">attested</span> {{.Actor}} · {{.At}} · “{{.Fact}}”</div>{{end}}
  {{range .Traces}}<div class="ask-trace">{{.At}} · {{.Actor}} · {{.Action}}{{with .Citation}} · citation {{.Type}}:{{.Ref}}{{end}}</div>{{end}}
</div>{{end}}
`
