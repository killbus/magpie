// Provider rules and model opt-outs are drafts, including empty opt-outs.
// APIs are fixtures; no installed credentials or live configuration are used.
const assert = require("node:assert/strict");
const fs = require("node:fs/promises");
const path = require("node:path");
const { test } = require("node:test");
const { chromium, webkit } = require("playwright");
const assets = path.resolve(__dirname, "../assets");

function serve(lang, posts) {
  const base = {
    icon: "generic", chat: "https://relay.example/v1", responses: "", anthropic: "", catalog: "",
    models: [{ id: "model-a", name: "Model A", on: true, strip: [], inheritStrip: false }],
    agents: [], headers: {}, fallback: [], key: { set: true, masked: "fixture" }, keyList: [],
    ready: true, balanceToken: { takes: false, set: false }, strip: ["metadata"], stripSupported: true,
  };
  const relay = { ...base, id: "relay", name: "Relay" };
  const empty = { ...base, id: "empty", name: "Empty", models: [] };
  const plugin = { ...base, id: "pluginco", name: "PluginCo", chat: "plugin://pluginco/v1", account: { agent: "plugin", agentName: "PluginCo", user: "fixture@example.com", plan: "" } };
  const builtin = { ...plugin, id: "builtin", name: "Builtin", stripSupported: false };
  const providers = { providers: [relay, empty, plugin, builtin], presets: [], excluded: [], gateway: { running: true, window: true } };
  const state = { agents: [], profiles: [], settings: { lang, theme: "light" } };
  return async (route) => {
    const url = new URL(route.request().url());
    const json = (data) => route.fulfill({ json: data });
    if (url.pathname === "/boot.js") return route.fulfill({ contentType: "text/javascript", body: `window.bootPrefs={lang:${JSON.stringify(lang)},theme:"light",web:true};` });
    if (url.pathname === "/wails/runtime.js") return route.fulfill({ contentType: "text/javascript", body: "export const Window={};" });
    if (url.pathname === "/api/state") return json(state);
    if (url.pathname === "/api/providers") return json(providers);
    if (url.pathname === "/api/groups") return json({ groups: [] });
    if (url.pathname.startsWith("/api/provider/")) {
      posts.push({ path: url.pathname, body: route.request().postDataJSON() });
      return json(providers);
    }
    if (url.pathname.startsWith("/api/")) return json({});
    const file = path.join(assets, url.pathname === "/" ? "index.html" : url.pathname);
    await route.fulfill({ body: await fs.readFile(file), contentType: { ".html": "text/html", ".js": "text/javascript", ".css": "text/css", ".svg": "image/svg+xml", ".png": "image/png" }[path.extname(file)] });
  };
}

for (const engine of (process.env.BROWSER ? [process.env.BROWSER] : ["chromium", "webkit"])) {
  for (const lang of ["en", "zh"]) {
    test(`${engine} ${lang}: provider strip drafts, model inheritance, empty and account providers`, async (t) => {
      const browser = await (engine === "webkit" ? webkit.launch() : chromium.launch({ channel: process.env.PLAYWRIGHT_CHANNEL || "chromium" }));
      t.after(() => browser.close());
      const page = await (await browser.newContext({ viewport: { width: 960, height: 760 }, reducedMotion: "reduce" })).newPage();
      page.setDefaultTimeout(5000);
      const posts = [], errors = [];
      page.on("pageerror", (e) => errors.push(e.message));
      await page.route("**/*", serve(lang, posts));
      await page.goto("http://magpie.test/?view=providers");
      const zh = lang === "zh";
      const L = { provider: zh ? "Provider 级剥离参数" : "Provider strip params", own: zh ? "剥离参数" : "Strip params", inherit: zh ? "继承 Provider 剥离规则" : "Inherit provider strip rules", names: zh ? "名称与推理档位" : "Names & levels", reset: zh ? "恢复默认" : "Restore default", save: zh ? "保存" : "Save", cancel: zh ? "取消" : "Cancel" };
      const open = async (name, names = true) => {
        await page.locator(".row.provider").filter({ hasText: name }).first().click();
        if (names && !(await page.locator(".mnames:not([hidden])").count())) await page.getByRole("button", { name: L.names, exact: true }).click();
      };
      const row = () => page.locator(".mname").first();
      const cancel = async () => page.getByRole("button", { name: L.cancel, exact: true }).click();
      const save = async () => { await page.getByRole("button", { name: L.save, exact: true }).click(); await page.waitForFunction(() => !document.querySelector(".editor")); };
      await open("Relay");
      assert.equal(await row().getByLabel(L.inherit, { exact: true }).isChecked(), false);
      assert(await row().getByRole("button", { name: L.reset, exact: true }).isVisible(), "empty opt-out is still custom");
      await row().getByRole("button", { name: L.reset, exact: true }).click();
      assert.equal(await row().getByLabel(L.inherit, { exact: true }).isChecked(), true);
      assert.match(await row().locator(".strip-effective").textContent(), /metadata/);
      await page.getByLabel(L.provider, { exact: true }).fill("reasoning_effort, reasoning.effort");
      assert.match(await row().locator(".strip-effective").textContent(), /reasoning_effort/);
      await row().getByLabel(L.own, { exact: true }).fill("output_config.effort");
      await row().getByLabel(L.inherit, { exact: true }).uncheck();
      assert.equal(await row().getByLabel(L.own, { exact: true }).inputValue(), "output_config.effort", "toggle does not copy or clear lists");
      assert.doesNotMatch(await row().locator(".strip-effective").textContent(), /reasoning_effort/);
      assert.deepEqual(posts, []);
      assert(await page.locator(".mnames").evaluate((e) => e.getBoundingClientRect().width > 400), "model controls occupy the content column, not the label column");
      if (process.env.ARTIFACT_DIR) {
        await fs.mkdir(process.env.ARTIFACT_DIR, { recursive: true });
        await page.screenshot({ path: path.join(process.env.ARTIFACT_DIR, `${engine}-${lang}-strip-desktop.png`) });
      }
      await page.setViewportSize({ width: 560, height: 760 });
      await page.getByLabel(L.provider, { exact: true }).scrollIntoViewIfNeeded();
      assert(await page.locator(".editor").evaluate((e) => e.scrollWidth <= e.clientWidth + 1), "narrow editor does not overflow");
      if (process.env.ARTIFACT_DIR) await page.screenshot({ path: path.join(process.env.ARTIFACT_DIR, `${engine}-${lang}-strip-narrow.png`) });
      await cancel();
      await page.setViewportSize({ width: 960, height: 760 });
      await open("Relay");
      assert.equal(await page.getByLabel(L.provider, { exact: true }).inputValue(), "metadata");
      assert.equal(await row().getByLabel(L.inherit, { exact: true }).isChecked(), false);
      assert.equal(await row().getByLabel(L.own, { exact: true }).inputValue(), "");
      await page.getByLabel(L.provider, { exact: true }).fill("bad..path");
      await page.getByRole("button", { name: L.save, exact: true }).click();
      assert.deepEqual(posts, [], "invalid path not submitted");
      await page.getByLabel(L.provider, { exact: true }).fill("reasoning_effort");
      await row().getByRole("button", { name: L.reset, exact: true }).click();
      await save();
      const relay = posts.pop().body;
      assert.deepEqual(relay.modelPrefs, { "model-a": { inheritStrip: true } });
      assert.deepEqual(relay.strip, ["reasoning_effort"]);
      await open("Relay");
      await row().getByLabel(L.own, { exact: true }).fill("output_config.effort");
      await save();
      const own = posts.pop().body;
      assert.equal(Object.hasOwn(own, "strip"), false, "unchanged provider list is omitted");
      assert.deepEqual(own.modelPrefs, { "model-a": { strip: ["output_config.effort"] } }, "editing paths does not overwrite saved inheritance");
      await open("Empty", false);
      await page.getByLabel(L.provider, { exact: true }).fill("");
      await save();
      assert.deepEqual(posts.pop().body.strip, []);
      await open("PluginCo");
      await page.getByLabel(L.provider, { exact: true }).fill("metadata, reasoning_effort");
      await save();
      const plugin = posts.pop().body;
      assert.deepEqual(plugin.strip, ["metadata", "reasoning_effort"]);
      assert.equal(plugin.id, "pluginco");
      await open("Builtin");
      assert(await page.getByLabel(L.provider, { exact: true }).isDisabled());
      assert(await row().getByLabel(L.inherit, { exact: true }).isDisabled());
      assert(await row().getByLabel(L.own, { exact: true }).isDisabled());
      await cancel();
      await open("Relay", false);
      await page.getByRole("button", { name: zh ? "复制" : "Duplicate", exact: true }).click();
      assert.equal(await page.getByLabel(L.provider, { exact: true }).inputValue(), "metadata", "copy starts with provider rules even before it exists");
      await page.getByLabel(L.provider, { exact: true }).fill("reasoning.effort");
      await page.getByRole("button", { name: zh ? "添加" : "Add", exact: true }).click();
      await page.waitForFunction(() => !document.querySelector(".editor"));
      const copy = posts.pop().body;
      assert.equal(copy.new, true);
      assert.equal(copy.copyOf, "relay");
      assert.deepEqual(copy.strip, ["reasoning.effort"]);
      assert.deepEqual(errors, []);
    });
  }
}
