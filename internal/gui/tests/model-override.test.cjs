// Run with Node's test runner and Playwright on the module path; see README.md.
// A model's Param Override editor: the right-click menu of a model in a
// provider's Names & levels opens one JSON box, the whole override — not a
// path-only delete field — and it says live why what is typed can't be saved
// (not JSON, not an object, the protected root model). A Save is held until
// it can be, a good one is sent for the model, and the inherit switch keeps
// the model out of its provider's override. It fits a narrow window. In
// English, Chinese, Japanese and German, Chromium and WebKit.
const assert = require("node:assert/strict");
const fs = require("node:fs/promises");
const path = require("node:path");
const { test } = require("node:test");
const { chromium, webkit } = require("playwright");

const assets = path.resolve(__dirname, "../assets");
const relay = {
  id: "relay", name: "Relay", preset: "", icon: "generic", chat: "https://relay.example.com/v1", responses: "", anthropic: "", gemini: "", catalog: "", website: "", keysUrl: "",
  headers: {}, extraBody: {}, baseAPI: "chat", searches: false, pinUpstream: false, unredacted: false, insecureTLS: false,
  takesInsecureTLS: false, limitRestOff: false, nativeCompaction: false, carriesCompaction: false,
  key: { set: true, masked: "sk-…one" }, keyList: [{ id: "k1", name: "", masked: "sk-…one", on: true, active: true }],
  balanceToken: { takes: false, set: false }, proxy: "", speaks: ["chat"], models: [
    { id: "gpt-5.1", name: "gpt-5.1", on: true, images: false, efforts: [], kept: [], given: false, override: { temperature: 0.2 }, overrideFrom: "own" },
    { id: "sol", name: "sol", on: true, images: false, efforts: [], kept: [], given: false },
  ], agents: [], fallback: [], fallbackGroups: [], unlisted: [],
};

function serve(lang, calls) {
  const providers = { providers: [relay], presets: [], excluded: [], gateway: { running: true, window: true } };
  const state = { agents: [], profiles: [], settings: { lang, theme: "light" } };
  return async (route) => {
    const url = new URL(route.request().url());
    const json = (data) => route.fulfill({ json: data });
    if (url.pathname === "/boot.js") return route.fulfill({ contentType: "text/javascript", body: `window.bootPrefs = {lang:"${lang}",theme:"light",web:true};` });
    if (url.pathname === "/wails/runtime.js") return route.fulfill({ contentType: "text/javascript", body: "export const Window = {};" });
    if (url.pathname === "/api/state") return json(state);
    if (url.pathname === "/api/providers") return json(providers);
    if (url.pathname === "/api/groups") return json({ groups: [] });
    if (url.pathname.startsWith("/api/provider/")) {
      const body = route.request().postDataJSON();
      calls.push([url.pathname.slice(14), body]);
      if (url.pathname === "/api/provider/test") return json({ results: [] });
      if (url.pathname === "/api/provider/save") return json(providers);
    }
    if (url.pathname.startsWith("/api/")) return json({});
    const file = path.join(assets, url.pathname === "/" ? "index.html" : url.pathname);
    const contentType = { ".html": "text/html", ".js": "text/javascript", ".css": "text/css", ".svg": "image/svg+xml", ".png": "image/png" }[path.extname(file)];
    try { await route.fulfill({ body: await fs.readFile(file), contentType }); } catch { await route.fulfill({ status: 404, body: "" }); }
  };
}

const words = {
  en: { names: "Names & levels", menu: "Param override…", head: "Param override for {id}", save: "Save", notjson: "not valid JSON", object: "write a JSON object", model: "protected", off: "Keep this model out of its provider's override" },
  zh: { names: "名称与等级", menu: "参数覆盖…", head: "「{id}」的参数覆盖", save: "保存", notjson: "不是有效的 JSON", object: "请写一个 JSON 对象", model: "受保护", off: "让这个模型不受供应商参数覆盖的影响" },
  ja: { names: "名前とレベル", menu: "パラメータ上書き…", head: "{id} のパラメータ上書き", save: "保存", notjson: "有効な JSON ではありません", object: "JSON オブジェクトを書いてください", model: "保護", off: "このモデルをプロバイダーの上書きの対象外にする" },
  de: { names: "Namen & Stufen", menu: "Parameter-Override…", head: "Parameter-Override für {id}", save: "Speichern", notjson: "kein gültiges JSON", object: "ein JSON-Objekt schreiben", model: "geschützt", off: "Dieses Modell vom Override seines Anbieters ausnehmen" },
};

for (const engine of (process.env.BROWSER ? [process.env.BROWSER] : ["chromium", "webkit"])) {
  for (const lang of Object.keys(words)) {
    const w = words[lang];
    test(`${engine} ${lang}: a model's Param Override is one JSON, checked, saved`, async (t) => {
      const browser = await (engine === "webkit" ? webkit.launch() : chromium.launch({ channel: "chromium" }));
      t.after(() => browser.close());
      const page = await (await browser.newContext({ viewport: { width: 900, height: 900 }, reducedMotion: "reduce" })).newPage();
      page.setDefaultTimeout(5000);
      const errors = [];
      page.on("pageerror", (e) => errors.push(e.message));
      const calls = [];
      await page.route("**/*", serve(lang, calls));
      await page.goto("http://magpie.test/?view=providers");

      // open the provider's editor, then its Names & levels, then the model's
      // right-click menu
      await page.locator(".row.provider", { hasText: "Relay" }).click();
      const ed = page.locator(".editor");
      await ed.locator(".mchips").first().waitFor();
      // the models are drawn as chips; open the first one's menu
      const chip = ed.locator(".mchips .mchip", { hasText: "gpt-5.1" }).first();
      await chip.waitFor({ timeout: 5000 });
      await chip.click({ button: "right" });
      const item = page.locator(".row-menu").getByText(w.menu);
      await item.waitFor({ timeout: 5000 });
      await item.click();

      const box = page.locator(".override-editor textarea");
      await box.waitFor();
      assert.ok(await page.getByText(w.head.replace("{id}", "gpt-5.1")).first().isVisible(), "headed by the model");
      // the saved override is shown
      assert.deepEqual(JSON.parse(await box.inputValue()), { temperature: 0.2 });
      const why = page.locator(".override-editor .why");
      assert.equal(await why.textContent(), "");

      const said = async (text, want) => {
        await box.fill(text);
        assert.equal(await box.getAttribute("aria-invalid"), "true", `${text} is marked`);
        assert.ok((await why.textContent()).includes(want), `${text}: ${await why.textContent()}`);
      };
      await said("[1, 2]", w.object);
      await said(`{"a":`, w.notjson);
      await said(`{"operations":[{"path":"model","mode":"delete"}]}`, w.model);

      // a Save is held while it can't be saved
      await page.locator(".override-editor").getByRole("button", { name: w.save, exact: true }).click();
      await page.waitForTimeout(200);
      assert.ok(await page.locator(".override-editor").isVisible(), "the editor stays open");
      assert.deepEqual(calls.map((c) => c[0]), [], "nothing saved");

      // the narrow window: the box and its reason stay inside
      await page.setViewportSize({ width: 420, height: 900 });
      const fits = await page.evaluate(() => {
        const ed = document.querySelector(".override-editor").getBoundingClientRect();
        return [...document.querySelectorAll(".override-editor textarea, .override-editor .why")].every((e) => {
          const r = e.getBoundingClientRect();
          return r.left >= ed.left - 0.5 && r.right <= ed.right + 0.5;
        });
      });
      assert.ok(fits, "fits a narrow window");
      await page.setViewportSize({ width: 900, height: 900 });

      // a good one saves the model's override
      const good = { operations: [{ path: "max_tokens", mode: "set", value: 8000 }] };
      await box.fill(JSON.stringify(good));
      await page.locator(".override-editor").getByRole("button", { name: w.save, exact: true }).click();
      for (let i = 0; i < 60 && !calls.some((c) => c[0] === "save"); i++) await page.waitForTimeout(50);
      const saved = calls.find((c) => c[0] === "save");
      assert.ok(saved, "saved");
      assert.deepEqual(saved[1].modelPrefs?.["gpt-5.1"]?.override, good, "the model's override was saved");

      // the inherit switch: reopening the model's editor, ticking it keeps
      // the model out of its provider's override, and the box is disabled
      // while it is on (the two are one choice, not two)
      calls.length = 0;
      await page.locator(".row.provider", { hasText: "Relay" }).click();
      const chip2 = page.locator(".editor .mchips .mchip", { hasText: "gpt-5.1" }).first();
      await chip2.click({ button: "right" });
      await page.locator(".row-menu").getByText(w.menu).click();
      const box2 = page.locator(".override-editor textarea");
      await box2.waitFor();
      const off = page.locator(".override-editor .tick", { hasText: w.off });
      await off.locator("input").check();
      assert.ok(await box2.isDisabled(), "the box is disabled while the model is kept out");
      await page.locator(".override-editor").getByRole("button", { name: w.save, exact: true }).click();
      for (let i = 0; i < 60 && !calls.some((c) => c[0] === "save"); i++) await page.waitForTimeout(50);
      const offSaved = calls.find((c) => c[0] === "save");
      assert.ok(offSaved, "the opt-out was saved");
      assert.equal(offSaved[1].modelPrefs?.["gpt-5.1"]?.overrideOff, true, "the model was kept out of its provider's override");

      assert.deepEqual(errors, []);
    });
  }
}
