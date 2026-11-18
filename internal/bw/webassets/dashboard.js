"use strict";
const $ = (id) => document.getElementById(id);
function el(tag, text, cls) {
  const e = document.createElement(tag);
  if (text !== undefined) e.textContent = text;
  if (cls) e.className = cls;
  return e;
}
function card(title) {
  const c = el("article", undefined, "card mb-3"),
    b = el("div", undefined, "card-body");
  if (title) b.append(el("h2", title, "card-title"));
  c.append(b);
  return [c, b];
}
function link(text, url) {
  if (!url) return el("span", text);
  const a = el("a", text);
  try {
    const u = new URL(url);
    if (!["http:", "https:"].includes(u.protocol)) return el("span", text);
    a.href = u.href;
    a.target = "_blank";
    a.rel = "noopener noreferrer";
  } catch {
    return el("span", text);
  }
  return a;
}
function table(headers, rows) {
  const wrap = el("div", undefined, "table-responsive"),
    t = el("table", undefined, "table table-vcenter");
  const head = el("thead"),
    tr = el("tr");
  headers.forEach((h) => tr.append(el("th", h)));
  head.append(tr);
  t.append(head);
  const body = el("tbody");
  rows.forEach((row) => {
    const tr = el("tr");
    row.forEach((value) => {
      const td = el("td");
      td.append(
        value instanceof Node
          ? value
          : document.createTextNode(String(value ?? "—")),
      );
      tr.append(td);
    });
    body.append(tr);
  });
  t.append(body);
  wrap.append(t);
  return wrap;
}
function duration(s) {
  return s >= 3600 ? (s / 3600).toFixed(1) + "h" : Math.round(s / 60) + "m";
}
function chart(title, points) {
  const [c, b] = card(title);
  if (!points.length) {
    b.append(el("p", "No events yet.", "text-secondary"));
    return c;
  }
  const ns = "http://www.w3.org/2000/svg",
    svg = document.createElementNS(ns, "svg");
  svg.setAttribute("viewBox", "0 0 700 240");
  svg.classList.add("chart");
  svg.setAttribute("role", "img");
  svg.setAttribute("aria-label", title);
  const maxY = Math.max(1, ...points.map((p) => p[1]));
  const first = points[0][0],
    span = Math.max(1, points.at(-1)[0] - first);
  const line = document.createElementNS(ns, "polyline");
  line.setAttribute(
    "points",
    points
      .map(
        ([x, y]) =>
          `${50 + ((x - first) / span) * 620},${200 - (y / maxY) * 165}`,
      )
      .join(" "),
  );
  svg.append(line);
  for (const [x, y, text] of [
    [8, 35, String(maxY)],
    [8, 200, "0"],
    [50, 230, new Date(first).toLocaleString()],
    [470, 230, new Date(points.at(-1)[0]).toLocaleString()],
  ]) {
    const t = document.createElementNS(ns, "text");
    t.setAttribute("x", x);
    t.setAttribute("y", y);
    t.textContent = text;
    svg.append(t);
  }
  b.append(svg);
  return c;
}
let current = null,
  answering = 0;
function render(d) {
  current = d;
  $("project-title").textContent=d.project || "Project dashboard";
  $("description").textContent=d.description || "";
  renderPlanning(d);
  $("identity").textContent = "As " + d.identity;
  $("status").textContent =
    d.summary.progress + " · refreshed " + new Date().toLocaleTimeString();
  $("error").hidden = true;
  const grid = el("div", undefined, "board-grid");
  for (const state of ["claimed", "ready", "waiting", "done"]) {
    const col = el("div");
    col.append(
      el(
        "h2",
        state.toUpperCase() + " · " + d.summary.counts[state],
        "h4 text-secondary",
      ),
    );
    for (const task of d.tasks.filter((t) => t.state === state)) {
      const [c, b] = card();
      c.classList.add("task-card");
      if (task.activity) c.dataset.activity=task.activity;
      const h = el("h3");
      h.append(link(task.id, task.links?.ref || task.links?.catalog));
      b.append(h, el("p", task.title));
      const badges = el("div", undefined, "d-flex flex-wrap gap-2 mb-2");
      for (const label of [task.size, task.effort, task.progress, ...(task.roles || [])])
        if (label) badges.append(el("span", label, "badge bg-blue-lt"));
      b.append(badges);
      if (task.holder)
        b.append(
          el(
            "p",
            (task.activity || task.state).toUpperCase() + " · " + task.holder +
              " · last " + (task.last_write || "write") + " " +
              (task.idle || task.age) +
              " ago" + (task.review_status ? " · " + task.review_status : ""),
            "text-secondary",
          ),
        );
      if (task.blocked_by.length)
        b.append(
          el(
            "p",
            "Waiting on " +
              task.blocked_by
                .map(
                  (t) =>
                    t.id +
                    " (" +
                    t.state +
                    (t.holder ? " by " + t.holder : "") +
                    ")",
                )
                .join(", "),
            "text-secondary",
          ),
        );
      if (task.steps?.length) {
        const list = el("ul");
        task.steps.forEach((s) =>
          list.append(
            el(
              "li",
              (task.completed_steps?.includes(s.id || s.text) ? "✓ " : "○ ") +
                s.text,
            ),
          ),
        );
        b.append(list);
      }
      col.append(c);
    }
    grid.append(col);
  }
  $("board").replaceChildren(grid);
  const stats = d.statistics;
  let completed = 0,
    done = new Set(),
    start = stats.run.started ? Date.parse(stats.run.started) : Date.now();
  const burn = [[start, stats.run.total]],
    through = [[start, 0]];
  for (const e of d.history) {
    if (e.kind === "done") {
      done.add(e.task);
      completed++;
    }
    if (e.kind === "reopened") done.delete(e.task);
    burn.push([Date.parse(e.at), stats.run.total - done.size]);
    through.push([Date.parse(e.at), completed]);
  }
  const end = stats.run.ended ? Date.parse(stats.run.ended) : Date.now();
  burn.push([end, stats.run.total - done.size]);
  through.push([end, completed]);
  $("progress").replaceChildren(
    chart("Remaining tasks · current catalog", burn),
    chart("Completed tasks over time · cumulative throughput", through),
  );
  const [tc, tb] = card("Wall time · estimates and actuals");
  tb.append(
    table(
      [
        "Task",
        "Size",
        "Held",
        "Ready waiting",
        "Dependency waiting",
        "Review waiting",
        "Changes requested",
      ],
      stats.tasks.map((t) => [
        link(t.id, t.links?.ref || t.links?.catalog),
        t.size || "unsized",
        duration(t.held_seconds),
        duration(t.ready_seconds),
        duration(t.waiting_seconds),
        duration(t.review_waiting_seconds || 0),
        `${t.rounds.filter((r) => (r.verdict || "").includes("changes")).length}/${t.rounds.length}`,
      ]),
    ),
  );
  $("time").replaceChildren(tc);
  renderAgents(d);
  const attention = $("attention");
  if (answering > 0) return;
  const active = document.activeElement,
    focusID = active?.dataset?.id,
    selection = focusID ? [active.selectionStart, active.selectionEnd] : null;
  const drafts = new Map(
    [...attention.querySelectorAll("textarea")].map((t) => [
      t.dataset.id,
      t.value,
    ]),
  );
  attention.replaceChildren();
  for (const q of d.questions) {
    const [c, b] = card("#" + q.id + " · " + q.task);
    b.append(
      el("p", q.note),
      el(
        "small",
        q.actor + " · " + new Date(q.ts).toLocaleString(),
        "text-secondary",
      ),
    );
    for (const a of q.answers)
      b.append(el("p", a.actor + ": " + a.note, "mt-3"));
    if (!q.answers.length) {
      const form = el("form", undefined, "mt-3"),
        input = el("textarea", undefined, "form-control mb-2");
      input.required = true;
      input.setAttribute("aria-label", "Answer question " + q.id);
      input.dataset.id = String(q.id);
      input.value = drafts.get(String(q.id)) || "";
      const submit = el("button", "Answer as " + d.identity, "btn btn-primary");
      submit.type = "submit";
      const feedback = el("p", undefined, "mt-2");
      feedback.setAttribute("role", "status");
      form.append(input, submit, feedback);
      form.onsubmit = async (event) => {
        event.preventDefault();
        answering++;
        submit.disabled = true;
        input.readOnly = true;
        try {
          const response = await fetch("/answer", {
            method: "POST",
            headers: { "Content-Type": "application/json" },
            body: JSON.stringify({ id: q.id, text: input.value }),
          });
          const result = await response.json();
          if (!result.ok) throw Error(result.error);
          input.value = "";
          feedback.textContent = "Answer saved.";
        } catch (e) {
          feedback.textContent = e.message;
        } finally {
          answering--;
          submit.disabled = false;
          input.readOnly = false;
        }
      };
      b.append(form);
    }
    attention.append(c);
  }
  const [rc, rb] = card("Reviews");
  rb.append(
    table(
      ["Review", "Task", "Effort", "Roles", "State", "Branch", "Commit"],
      d.reviews.map((r) => [
        link(r.id, r.links?.search),
        link(
          r.task,
          d.tasks.find((t) => t.id === r.task)?.links?.ref ||
            d.tasks.find((t) => t.id === r.task)?.links?.catalog,
        ),
        r.effort,
        (r.roles||[]).join(", "),
        r.state,
        link(r.branch, r.links?.branch),
        link(r.head.slice(0, 12), r.links?.head),
      ]),
    ),
  );
  for (const r of d.reviews) {
    rb.append(link("Find merge request for " + r.branch, r.links?.search));
    if (r.links?.merged)
      rb.append(" · ", link("Merged commit", r.links.merged));
    rb.append(el("br"));
  }
  attention.append(rc);
  const [nc, nb] = card("Notes");
  for (const n of d.notes)
    nb.append(el("p", n.task + " · " + n.actor + ": " + n.note));
  attention.append(nc);
  if (focusID) {
    const restored = [...attention.querySelectorAll("textarea")].find(
      (t) => t.dataset.id === focusID,
    );
    if (restored) {
      restored.focus();
      restored.setSelectionRange(...selection);
    }
  }
}
for (const button of document.querySelectorAll("[data-view]"))
  button.onclick = () => {
    for (const section of ["board", "todo", "shape", "progress", "time", "workers", "attention"])
      $(section).hidden = section !== button.dataset.view;
    document
      .querySelectorAll("[data-view]")
      .forEach((b) => b.classList.toggle("active", b === button));
  };
const source = new EventSource("/events");
source.onmessage = (e) => {
  try {
    const result = JSON.parse(e.data);
    if (!result.ok) throw Error(result.error);
    render(result.data);
  } catch (error) {
    $("error").hidden = false;
    $("error").textContent =
      error.message + " · showing last successful snapshot";
  }
};
source.onerror = () => {
  $("error").hidden = false;
  $("error").textContent =
    "Connection interrupted · reconnecting; showing last successful snapshot";
};

function renderPlanning(d) {
  const resources=d.resources;
  const banner=$("resources-banner");
  banner.hidden=!["low_resources","critical_resources"].includes(resources.status);
  banner.textContent=resources.status + ": " + (resources.reason || "");
  banner.className=resources.status==="critical_resources" ? "alert alert-danger" : "alert alert-warning";
  const [tc,tb]=card("What’s left · " + d.todo.tasks.length + " tasks · " + d.todo.useful_workers + " useful workers now");
  tb.append(table(["State","Task","Title","Holder","Size","Effort","Roles","Steps","Waiting on"],d.todo.tasks.map(t=>[t.activity||t.state,link(t.id,t.links?.ref||t.links?.catalog),t.title,t.holder,t.size,t.effort,(t.roles||[]).join(", "),t.progress,t.blocked_by.map(b=>b.id).join(", ")])));
  $("todo").replaceChildren(tc);
  const [sc,sb]=card("Plan shape");
  sb.append(el("p","Longest chain · " + d.shape.chain_length + ": " + d.shape.longest_chain.join(" → ")),
    el("p","Most ready at once · " + d.shape.most_ready + "; suggested crew · " + d.shape.suggested_crew),
    el("p","Waiting on four or more: " + d.shape.fan_in.join(", ")),
    el("p","Blocking four or more: " + d.shape.bottlenecks.join(", ")));
  for (const warning of d.shape.warnings) sb.append(el("p",warning,"text-warning"));
  const [rc,rb]=card("Resources · " + resources.status);
  rb.append(el("p","Free disk " + resources.free_disk_gb.toFixed(2) + " GB · available memory " + resources.free_memory_gb.toFixed(2) + " GB · " + resources.claims + " claims"));
  rb.append(table(["Limit","Value","Source","Override"],Object.entries(resources.levels).map(([k,v])=>[k,v.value || (k==="max_claims" ? "unlimited" : 0),v.source,v.variable])));
  if (resources.reason) rb.append(el("p",resources.reason));
  $("shape").replaceChildren(sc,rc);
}
function renderAgents(d) {
 const expanded=new Set([...$("workers").querySelectorAll("details[open]")].map(v=>v.dataset.key));
 const [wc,wb]=card("Agents · recorded activity");
 wb.append(table(["Agent","Activity","Roles","Effort","Done","Held time","Last activity"],d.workers.map(w=>[w.worker,w.activity + (w.current.length ? " · " + w.current.map(t=>t.id + " · " + (t.activity||t.state) + (t.progress ? " · " + t.progress : "")).join(", ") : ""),(w.roles||[]).join(", "),w.effort,w.completed,duration(w.held_seconds),new Date(w.last_activity).toLocaleString()])));
 for (const w of d.workers) {
  const detail=el("details"),summary=el("summary",w.worker + " · " + w.activity);
  detail.dataset.key="worker:"+w.worker;
  detail.open=expanded.has(detail.dataset.key);
  detail.append(summary,el("p","First " + new Date(w.first_activity).toLocaleString() + " · last " + new Date(w.last_activity).toLocaleString() + " · " + w.released + " released · " + w.held + " held · " + w.questions + " questions · " + w.notes + " notes"));
  for (const t of w.tasks) {
   const task=el("details"),title=el("summary");
   task.dataset.key="task:"+w.worker+":"+t.id+":"+t.claimed;
   task.open=expanded.has(task.dataset.key);
   title.append(link(t.id,t.links?.ref||t.links?.catalog), " · " + t.title + " · " + (t.size||"unsized") + " · " + t.outcome);
   task.append(title,el("p",new Date(t.claimed).toLocaleString()+" → "+(t.ended ? new Date(t.ended).toLocaleString() : "now")+" · "+duration(t.seconds)+" · review "+duration(t.review_waiting_seconds)));
   task.append(table(["Step","Duration"],t.steps.map(s=>[s.text,s.known ? duration(s.seconds) : "unknown · "+s.reason])));
   for (const r of t.rounds) {const review=d.reviews.find(v=>v.id===r.review);task.append(link(r.review,r.links?.search||review?.links?.search),el("p",(r.verdict||"pending")+" · "+(r.reviewer||"waiting for reviewer")+" · "+duration(r.seconds)));}
   detail.append(task);
  }
  for (const r of w.reviews) {const row=el("p");row.append(link(r.id,r.links?.search)," · "+r.state+" · ",link(r.branch,r.links?.branch)," · ",link(r.head.slice(0,12),r.links?.head));if(r.links?.merged)row.append(" · ",link("Merged commit",r.links.merged));detail.append(row);}
  for (const v of w.verdicts) {const row=el("p");row.append(link(v.review,d.reviews.find(r=>r.id===v.review)?.links?.search)," · "+v.verdict);detail.append(row);}
  wb.append(detail);
 }
 $("workers").replaceChildren(wc);
}
