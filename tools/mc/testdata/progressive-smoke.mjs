import {spawn} from "node:child_process";
import {mkdtemp, rm} from "node:fs/promises";
import {tmpdir} from "node:os";
import {join} from "node:path";
import {createServer} from "node:net";

const [chrome, baseURL] = process.argv.slice(2);
if (!chrome || !baseURL) throw new Error("usage: progressive-smoke.mjs CHROME BASE_URL");

const delay = (milliseconds) => new Promise((resolve) => setTimeout(resolve, milliseconds));

async function freePort() {
  const server = createServer();
  await new Promise((resolve, reject) => server.listen(0, "127.0.0.1", resolve).once("error", reject));
  const {port} = server.address();
  await new Promise((resolve) => server.close(resolve));
  return port;
}

class CDP {
  constructor(socket) {
    this.socket = socket;
    this.nextID = 1;
    this.pending = new Map();
    socket.addEventListener("message", (event) => {
      const message = JSON.parse(event.data);
      if (!message.id) {
        if (message.method === "Runtime.exceptionThrown") {
          process.stderr.write(`browser exception: ${message.params.exceptionDetails.text}\n`);
        }
        if (message.method === "Runtime.consoleAPICalled") {
          const values = message.params.args.map((argument) => argument.value || argument.description).join(" ");
          process.stderr.write(`browser console ${message.params.type}: ${values}\n`);
        }
        return;
      }
      const pending = this.pending.get(message.id);
      if (!pending) return;
      this.pending.delete(message.id);
      if (message.error) pending.reject(new Error(message.error.message));
      else pending.resolve(message.result);
    });
  }

  static async connect(url) {
    const socket = new WebSocket(url);
    await new Promise((resolve, reject) => {
      socket.addEventListener("open", resolve, {once: true});
      socket.addEventListener("error", reject, {once: true});
    });
    return new CDP(socket);
  }

  send(method, params = {}) {
    const id = this.nextID++;
    return new Promise((resolve, reject) => {
      this.pending.set(id, {resolve, reject});
      this.socket.send(JSON.stringify({id, method, params}));
    });
  }

  async evaluate(expression) {
    const result = await this.send("Runtime.evaluate", {expression, returnByValue: true, awaitPromise: true});
    if (result.exceptionDetails) throw new Error(result.exceptionDetails.text || "browser evaluation failed");
    return result.result.value;
  }

  close() {
    this.socket.close();
  }
}

async function waitFor(check, message, timeout = 9000) {
  const deadline = Date.now() + timeout;
  let last;
  while (Date.now() < deadline) {
    try {
      last = await check();
      if (last) return last;
    } catch (error) {
      last = error.message;
    }
    await delay(100);
  }
  throw new Error(`${message}; last=${last}`);
}

async function key(cdp, value, code) {
  await cdp.send("Input.dispatchKeyEvent", {type: "keyDown", key: value, code, text: value.length === 1 ? value : undefined});
  await cdp.send("Input.dispatchKeyEvent", {type: "keyUp", key: value, code});
}

const port = await freePort();
const profile = await mkdtemp(join(tmpdir(), "mc-chrome-"));
const child = spawn(chrome, [
  "--headless=new", "--no-sandbox", "--disable-gpu", "--disable-dev-shm-usage",
  "--remote-allow-origins=*", `--remote-debugging-port=${port}`,
  `--user-data-dir=${profile}`, `${baseURL}/`,
], {stdio: "ignore"});

let cdp;
try {
  const page = await waitFor(async () => {
    const response = await fetch(`http://127.0.0.1:${port}/json/list`);
    const pages = await response.json();
    return pages.find((candidate) => candidate.type === "page" && candidate.url.startsWith(baseURL));
  }, "Chrome DevTools page did not appear");
  cdp = await CDP.connect(page.webSocketDebuggerUrl);
  await cdp.send("Runtime.enable");
  await cdp.send("Page.enable");
  await waitFor(() => cdp.evaluate("document.readyState === 'complete' && document.title.startsWith('(1)')"), "progressive layer did not initialize");

  await key(cdp, "j", "KeyJ");
  if (!await cdp.evaluate("document.activeElement?.href?.includes('peek=smoke')")) {
    throw new Error("j did not focus a native triage link");
  }
  await key(cdp, "Enter", "Enter");
  await waitFor(() => cdp.evaluate("location.search.includes('peek=smoke') && document.documentElement.dataset.mcEnhanced === 'true' && !!document.querySelector('[data-live=\"thread-tail-smoke\"]')"), "enter did not navigate to the enhanced thread");

  await fetch(`${baseURL}/__smoke/append`, {method: "POST", body: new URLSearchParams({text: "live-tail"})});
  await waitFor(() => cdp.evaluate("document.querySelector('[data-live=\"thread-tail-smoke\"]')?.textContent.includes('live-tail')"), "poll did not append the live thread tail");

  await cdp.evaluate(`(() => {
    const field = document.querySelector('.composer textarea');
    field.value = 'draft kept';
    field.focus();
    return true;
  })()`);
  await fetch(`${baseURL}/__smoke/append`, {method: "POST", body: new URLSearchParams({text: "while-typing"})});
  await waitFor(() => cdp.evaluate("document.querySelector('[data-live=\"thread-tail-smoke\"]')?.textContent.includes('while-typing')"), "tail did not update while typing");
  if (!await cdp.evaluate("document.querySelector('.composer textarea').value === 'draft kept' && document.activeElement === document.querySelector('.composer textarea')")) {
    throw new Error("poll replaced the focused, non-empty composer");
  }

  await cdp.evaluate(`(() => {
    const field = document.querySelector('.composer textarea');
    field.value = 'browser reply';
    window.__smokeNoReload = {alive: true};
    field.form.requestSubmit();
    return true;
  })()`);
  await waitFor(() => cdp.evaluate("document.querySelector('[data-live=\"thread-tail-smoke\"]')?.textContent.includes('browser reply')"), "fetch-submit reply did not morph the server-rendered tail");
  if (!await cdp.evaluate("window.__smokeNoReload?.alive === true")) {
    throw new Error("fetch-submit caused a full page reload");
  }

  await cdp.send("Page.addScriptToEvaluateOnNewDocument", {source: "window.__mcInjectError = true;"});
  await cdp.send("Page.navigate", {url: `${baseURL}/`});
  await waitFor(() => cdp.evaluate("document.readyState === 'complete' && window.__mcInjectError === true"), "exception-fallback page did not load");
  if (!await cdp.evaluate("!!document.querySelector('.rail-list a[href*=\"peek=smoke\"]') && !!document.querySelector('header a[href=\"/threads\"]')")) {
    throw new Error("injected exception removed native controls");
  }
  await cdp.evaluate("document.querySelector('.rail-list a[href*=\"peek=smoke\"]').click()")
  await waitFor(() => cdp.evaluate("location.search.includes('peek=smoke')"), "native navigation failed after injected JS exception");
} finally {
  cdp?.close();
  child.kill("SIGTERM");
  const exited = new Promise((resolve) => child.once("exit", resolve));
  await Promise.race([
    exited,
    delay(1000).then(() => {
      child.kill("SIGKILL");
      return exited;
    }),
  ]);
  for (let attempt = 0; attempt < 5; attempt++) {
    try {
      await rm(profile, {recursive: true, force: true, maxRetries: 3, retryDelay: 50});
      break;
    } catch (error) {
      if (attempt === 4) throw error;
      await delay(100);
    }
  }
}
