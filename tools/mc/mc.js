(() => {
  "use strict";

  if (window.__mcInjectError) throw new Error("injected mc startup failure");

  const POLL_MS = 5000;
  let etag = "";
  let pollTimer = 0;

  const parser = new DOMParser();
  const liveSelector = "[data-live]";
  const textEntrySelector = "input, textarea, select, [contenteditable]";

  function hasUserText(region) {
    return [...region.querySelectorAll("textarea")].some((field) => field.value !== "");
  }

  function replaceLiveRegions(nextDocument) {
    if (document.body.dataset.view !== nextDocument.body.dataset.view) return false;

    const active = document.activeElement;
    const windowX = window.scrollX;
    const windowY = window.scrollY;
    let replaced = 0;

    for (const current of document.querySelectorAll(liveSelector)) {
      const name = current.dataset.live;
      const next = [...nextDocument.querySelectorAll(liveSelector)]
        .find((candidate) => candidate.dataset.live === name);
      if (!next || current.contains(active) || hasUserText(current)) continue;

      const top = current.scrollTop;
      const left = current.scrollLeft;
      current.replaceChildren(...next.cloneNode(true).childNodes);
      current.scrollTop = top;
      current.scrollLeft = left;
      replaced++;
    }

    window.scrollTo(windowX, windowY);
    document.title = nextDocument.title;
    updateUnreadTitle();
    tickRelativeTimes();
    autosizeAll();
    return replaced > 0;
  }

  async function fetchPage(url, options = {}) {
    const headers = new Headers(options.headers || {});
    headers.set("Accept", "text/html");
    if (options.poll && etag) headers.set("If-None-Match", etag);

    const response = await fetch(url, {...options, headers});
    if (response.status === 304) return {response, changed: false};
    if (!response.ok) throw new Error(`mc request failed: ${response.status}`);

    const responseETag = response.headers.get("ETag");
    if (responseETag) etag = responseETag;
    const nextDocument = parser.parseFromString(await response.text(), "text/html");
    return {response, nextDocument, changed: replaceLiveRegions(nextDocument)};
  }

  async function poll() {
    if (document.hidden) return;
    try {
      await fetchPage(location.href, {poll: true, cache: "no-cache"});
    } catch (error) {
      console.warn("mc live refresh failed", error);
    } finally {
      schedulePoll();
    }
  }

  function schedulePoll() {
    clearTimeout(pollTimer);
    if (!document.hidden) pollTimer = window.setTimeout(poll, POLL_MS);
  }

  function nativeSubmit(form, submitter) {
    form.dataset.mcNative = "1";
    if (submitter) form.requestSubmit(submitter);
    else form.submit();
  }

  document.addEventListener("submit", async (event) => {
    const form = event.target;
    if (!(form instanceof HTMLFormElement) || form.dataset.mcNative ||
        (event.submitter?.formMethod || form.method).toLowerCase() !== "post") return;

    event.preventDefault();
    const submitter = event.submitter;
    const action = submitter?.formAction || form.action;
    const body = new URLSearchParams(new FormData(form, submitter));

    try {
      const result = await fetchPage(action, {
        method: "POST",
        body,
        headers: {"Content-Type": "application/x-www-form-urlencoded;charset=UTF-8"},
      });
      if (!result.nextDocument || document.body.dataset.view !== result.nextDocument.body.dataset.view) {
        location.assign(result.response.url);
        return;
      }
      history.replaceState(null, "", result.response.url);
      for (const field of form.querySelectorAll("textarea")) field.value = "";
      autosizeAll();
    } catch (error) {
      console.warn("mc enhanced submit failed; using native submit", error);
      nativeSubmit(form, submitter);
    }
  });

  function triageLinks() {
    return [...document.querySelectorAll(
      ".rail:not(.thread-local) .rail-list a, main .card a.title"
    )].filter((link) => link.offsetParent !== null);
  }

  document.addEventListener("keydown", (event) => {
    const target = event.target;
    const typing = target instanceof Element && target.matches(textEntrySelector);

    if (event.ctrlKey && event.key === "Enter" && target instanceof HTMLTextAreaElement) {
      event.preventDefault();
      target.form?.requestSubmit();
      return;
    }
    if (typing || event.ctrlKey || event.metaKey || event.altKey) return;

    if (event.key === "r") {
      const composer = document.querySelector(".composer textarea");
      if (composer) {
        event.preventDefault();
        composer.focus();
      }
      return;
    }

    const links = triageLinks();
    if (!links.length) return;
    const current = links.indexOf(document.activeElement);
    if (event.key === "j" || event.key === "k") {
      event.preventDefault();
      const direction = event.key === "j" ? 1 : -1;
      const next = current < 0 ? (direction > 0 ? 0 : links.length - 1) :
        (current + direction + links.length) % links.length;
      links[next].focus();
    } else if (event.key === "Enter" && current >= 0) {
      event.preventDefault();
      links[current].click();
    }
  });

  function relativeLabel(date) {
    const seconds = Math.max(0, Math.floor((Date.now() - date.getTime()) / 1000));
    if (seconds < 10) return "now";
    if (seconds < 60) return `${seconds}s ago`;
    if (seconds < 3600) return `${Math.floor(seconds / 60)}m ago`;
    if (seconds < 86400) return `${Math.floor(seconds / 3600)}h ago`;
    return `${Math.floor(seconds / 86400)}d ago`;
  }

  function tickRelativeTimes() {
    for (const element of document.querySelectorAll("time[data-relative]")) {
      const date = new Date(element.dateTime);
      if (!Number.isNaN(date.getTime())) element.textContent = relativeLabel(date);
    }
  }

  function autosize(field) {
    field.style.height = "auto";
    field.style.height = `${field.scrollHeight}px`;
  }

  function autosizeAll() {
    for (const field of document.querySelectorAll("textarea")) autosize(field);
  }

  document.addEventListener("input", (event) => {
    if (event.target instanceof HTMLTextAreaElement) autosize(event.target);
  });

  function updateUnreadTitle() {
    const count = document.querySelectorAll(".rail .badge.yourturn").length;
    document.title = document.title.replace(/^\(\d+\)\s+/, "");
    if (count) document.title = `(${count}) ${document.title}`;
  }

  document.addEventListener("visibilitychange", () => {
    clearTimeout(pollTimer);
    if (!document.hidden) poll();
  });

  tickRelativeTimes();
  autosizeAll();
  updateUnreadTitle();
  document.documentElement.dataset.mcEnhanced = "true";
  schedulePoll();
})();
