// Run with Node's test runner and Playwright on the module path; see README.md.
// This file is deliberately self-contained: it tests the fork's param
// override paths with its own API fixtures and imports no shared test helper.
// It covers the provider/model editors, their save payload, the model badge,
// Routing's per-operation trace, and Recent calls' operation count. The API
// is faked and no user settings or live gateway are read.
const assert = require("node:assert/strict");
const fs = require("node:fs/promises");
const path = require("node:path");
const { test } = require("node:test");
const { chromium, webkit } = require("playwright");

const assets = path.resolve(__dirname, "../assets");
const languages = ["en", "zh", "zh-TW", "ja", "de"];
const engines = process.env.BROWSER ? [process.env.BROWSER] : ["chromium", "webkit"];
const now = new Date();
const day = [now.getFullYear(), now.getMonth() + 1, now.getDate()].map((n) => String(n).padStart(2, "0")).join("-");
const overrideLines = ["delete temperature", "move a -> b"];
const key = { id: "relay", provider: "relay", name: "Relay", kind: "provider", model: "sol" };
const route = {
  id: 41, seq: 41, time: now.toISOString(), agent: "codex", model: "relay/sol", provider: "relay", order: [key],
  tries: [{ id: key.id, model: key.model, start: now.toISOString(), done: true, status: 200, ms: 120, overrides: overrideLines }],
  done: true, status: 200, ms: 120,
};

const models = [
  { id: "sol", name: "sol", on: true, override: { temperature: 0.2 }, overrideFrom: "own" },
  { id: "luna", name: "luna", on: true, overrideFrom: "provider" },
  { id: "mars", name: "mars", on: true, overrideOff: true },
  { id: "venus", name: "venus", on: true, overrideFrom: "provider" },
];
const relay = {
  id: "relay", name: "Relay", icon: "generic", chat: "https://relay.example.com/v1", responses: "", anthropic: "", catalog: "",
  override: { temperature: 0.1 }, models, agents: [], fallback: [], headers: {},
  key: { set: true, masked: "sk-one" }, keyList: [], balanceToken: { takes: false, set: false }, proxy: "",
};

async function staticFile(route, url) {
  const file = path.join(assets, url.pathname === "/" ? "index.html" : url.pathname);
  const contentType = { ".html": "text/html", ".js": "text/javascript", ".css": "text/css", ".svg": "image/svg+xml", ".png": "image/png" }[path.extname(file)];
  return route.fulfill({ body: await fs.readFile(file), contentType });
}

function providersAPI(lang, saves, gatewayCalls = []) {
  const state = { agents: [], profiles: [], settings: { lang, theme: "light" } };
  const providers = { providers: [relay], presets: [], excluded: [], gateway: { running: true, window: true, mine: true, calls: gatewayCalls, groups: [] } };
  return async (route) => {
    const req = route.request(), url = new URL(req.url());
    const json = (data) => route.fulfill({ json: data });
    if (url.pathname === "/boot.js") return route.fulfill({ contentType: "text/javascript", body: `window.bootPrefs = {lang:${JSON.stringify(lang)},theme:"light",web:true};` });
    if (url.pathname === "/wails/runtime.js") return route.fulfill({ contentType: "text/javascript", body: "export const Window = {};" });
    if (url.pathname === "/api/state") return json(state);
    if (url.pathname === "/api/plugins") return json({ plugins: [] });
    if (url.pathname === "/api/providers") return json(providers);
    if (url.pathname === "/api/provider/save") {
      saves.push(req.postDataJSON());
      return json(providers);
    }
    if (url.pathname === "/api/groups") return json({ groups: [], models: [] });
    if (url.pathname.startsWith("/api/")) return json({});
    return staticFile(route, url);
  };
}

function routingAPI(lang) {
  const state = { agents: [{ id: "codex", name: "Codex", path: "/test/config.toml", fields: [] }], profiles: [], settings: { lang, theme: "light" } };
  return async (route) => {
    const url = new URL(route.request().url());
    const json = (data) => route.fulfill({ json: data });
    if (url.pathname === "/boot.js") return route.fulfill({ contentType: "text/javascript", body: `window.bootPrefs = {lang:${JSON.stringify(lang)},theme:"light",web:true};` });
    if (url.pathname === "/wails/runtime.js") return route.fulfill({ contentType: "text/javascript", body: "export const Window = {};" });
    if (url.pathname === "/api/state") return json(state);
    if (url.pathname === "/api/plugins") return json({ plugins: [] });
    if (url.pathname === "/api/providers") return json({ providers: [], presets: [], excluded: [], gateway: { running: true, window: true } });
    if (url.pathname === "/api/gateway/trace") return json({ mine: true, now: now.toISOString(), seq: 1, totals: { requests: 0, rerouted: 0, errors: 0 }, routes: [] });
    if (url.pathname === "/api/gateway/history") {
      return json({ cut: false, days: [{ day, requests: 1 }], routes: url.searchParams.get("day") ? [route] : [] });
    }
    if (url.pathname === "/api/groups") return json({ groups: [], models: [] });
    if (url.pathname.startsWith("/api/")) return json({});
    return staticFile(route, url);
  };
}

async function open(engine, lang, route, url, browser) {
  const page = await browser.newPage({ viewport: { width: 900, height: 760 }, reducedMotion: "reduce" });
  page.setDefaultTimeout(5000);
  page.setDefaultNavigationTimeout(60000);
  const errors = [];
  page.on("pageerror", (e) => errors.push(e.message));
  await page.route("**/*", route);
  await page.goto(url);
  return { page, errors };
}

const missingKeys = [
  "Param override: {line}",
  "1 param override operation changed this request: {lines}",
  "{n} param override operations changed this request: {lines}",
  "override",
  "no override",
  "Param override for {id}",
  "Param override JSON",
  "the root model field is protected and can't be overridden",
  "write a JSON object, like {\"temperature\": 0.2}",
  "Keep this model out of its provider's override",
  "{model}'s param override is made with the Save",
  "Param override…",
  "Add a param override…",
  "Adjust this model's requests at the wire with one JSON, before the body is signed",
  "Param override",
  "JSON applied to every request to every model of this provider, at the wire and before it is signed. A model's own wins over it, and a model can be kept out of it from its chip's menu.",
  "Param override: {why}",
];

async function wordsFor(page, lang, keys) {
  const found = await page.evaluate(({ lang, keys }) => Object.fromEntries(keys.map((key) => [key, I18N[lang]?.[key]])), { lang, keys });
  assert.deepEqual(Object.entries(found).filter(([, value]) => !value), [], `${lang} translations`);
  return found;
}

async function assertFits(page, box) {
  assert.equal(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth), true, "the page fits the width");
  const rect = await box.boundingBox(), view = await page.evaluate(() => innerWidth);
  assert.ok(rect && rect.x >= 0 && rect.x + rect.width <= view + 1, "the dialog fits the width");
}

for (const engine of engines) {
  test(`${engine}: param override stays on the fork's own GUI path`, async (t) => {
    const browser = await (engine === "webkit" ? webkit.launch() : chromium.launch({ channel: "chromium" }));
    t.after(() => browser.close());

    for (const lang of languages) {
      await t.test(`${lang}: provider and model editors save one payload`, async () => {
        const saves = [];
        const { page, errors } = await open(engine, lang, providersAPI(lang, saves), "http://magpie.test/?view=providers", browser);
        try {
          const w = await wordsFor(page, lang, [
            "override", "no override", "Add a param override…", "Param override for {id}",
            "Its provider has one for every model.", "Keep this model out of its provider's override",
            "Save", "the root model field is protected and can't be overridden",
            "{model}'s param override is made with the Save", "Param override JSON",
          ]);
          const chip = (id) => page.locator(".editor .mchips .mchip", { hasText: id });
          const tag = (id) => chip(id).locator(".moverride-tag");

          await page.locator(".row.provider", { hasText: "Relay" }).click();
          await chip("sol").waitFor();
          assert.equal(await tag("sol").innerText(), w.override);
          assert.equal(await tag("luna").innerText(), w.override);
          assert.equal(await tag("mars").innerText(), w["no override"]);
          assert.equal(await tag("venus").innerText(), w.override);

          let editor = page.locator("#modal:not([hidden]) .editor");
          const advanced = editor.locator("details.advanced");
          if (!(await advanced.evaluate((d) => d.open))) await advanced.locator(":scope > summary").click();
          const providerBox = advanced.locator(".override-editor textarea");
          assert.equal(await providerBox.getAttribute("aria-label"), w["Param override JSON"]);
          assert.equal(JSON.parse(await providerBox.inputValue()).temperature, 0.1);
          await providerBox.fill('{"temperature":0.15}');

          await chip("luna").click({ button: "right" });
          await page.locator(".pop.row-menu").getByRole("menuitem", { name: w["Add a param override…"] }).click();
          let modelEditor = page.locator("#modal:not([hidden]) .editor");
          assert.equal(await modelEditor.locator(".ehead b").innerText(), w["Param override for {id}"].replace("{id}", "luna"));
          await modelEditor.getByText(w["Its provider has one for every model."], { exact: false }).waitFor();
          const modelBox = modelEditor.locator(".override-editor textarea");

          await modelBox.fill('{"model":"customer/models/sol"}');
          assert.equal(await modelBox.getAttribute("aria-invalid"), "true");
          await modelEditor.getByRole("button", { name: w.Save, exact: true }).click();
          assert.equal(await page.locator("#status").innerText(), w["the root model field is protected and can't be overridden"]);
          assert.equal(await page.locator("#modal").isVisible(), true, "a refused override keeps the dialog open");

          await modelBox.fill('{"operations":[{"path":"temperature","mode":"set","value":0.3}]}');
          assert.equal(await modelBox.getAttribute("aria-invalid"), "false");
          const keep = modelEditor.locator("label.tick", { hasText: w["Keep this model out of its provider's override"] }).locator("input");
          await keep.check();
          assert.equal(await modelBox.isDisabled(), true, "the opt-out disables its own override box");
          await modelEditor.getByRole("button", { name: w.Save, exact: true }).click();

          const staged = w["{model}'s param override is made with the Save"].replace("{model}", "luna");
          await page.waitForFunction((want) => document.querySelector("#status")?.textContent === want, staged);
          assert.equal(await page.locator("#status").innerText(), staged);
          editor = page.locator("#modal:not([hidden]) .editor");
          const again = editor.locator("details.advanced");
          if (!(await again.evaluate((d) => d.open))) await again.locator(":scope > summary").click();
          assert.equal(JSON.parse(await again.locator(".override-editor textarea").inputValue()).temperature, 0.15, "the provider draft survives the model dialog");

          await page.setViewportSize({ width: 420, height: 760 });
          await assertFits(page, editor);
          await editor.locator(".bar").getByRole("button", { name: w.Save, exact: true }).click();
          for (let i = 0; i < 60 && saves.length === 0; i++) await page.waitForTimeout(50);
          assert.equal(saves.length, 1, "one provider save");
          assert.deepEqual(saves[0].modelPrefs["*"], { override: { temperature: 0.15 } });
          assert.deepEqual(saves[0].modelPrefs.luna, {
            override: { operations: [{ path: "temperature", mode: "set", value: 0.3 }] },
            overrideOff: true,
          });
          assert.deepEqual(errors, []);
        } finally {
          await page.close();
        }
      });

      await t.test(`${lang}: Routing and Recent calls show what changed`, async () => {
        const { page, errors } = await open(engine, lang, routingAPI(lang), "http://magpie.test/?view=routing", browser);
        try {
          const w = await wordsFor(page, lang, [
            "Param override: {line}",
            "1 param override operation changed this request: {lines}",
            "{n} param override operations changed this request: {lines}",
          ]);
          await page.locator(".rt-day").nth(1).click();
          await page.locator(".rt-req").first().waitFor();
          await page.locator(".rt-req").first().click();
          const steps = await page.locator(".rt-steps").innerText();
          for (const line of overrideLines) assert(steps.includes(w["Param override: {line}"].replace("{line}", line)), steps);

          const calls = [{ id: 41, time: now.toISOString(), agent: "codex", model: "relay/sol", from: "chat", to: "chat", status: 200, ms: 120, overrides: overrideLines }];
          const gateway = await open(engine, lang, providersAPI(lang, [], calls), "http://magpie.test/?view=gateway", browser);
          try {
            const badge = gateway.page.locator("#activity .override-badge").first();
            await badge.waitFor();
            const title = await badge.getAttribute("title");
            assert(title.includes(w["{n} param override operations changed this request: {lines}"].replace("{n}", "2").replace("{lines}", overrideLines.join("; "))), title);
            assert.deepEqual(gateway.errors, []);
          } finally {
            await gateway.page.close();
          }
          assert.deepEqual(errors, []);
        } finally {
          await page.close();
        }
      });

      if (lang === "en") {
        const { page } = await open(engine, lang, providersAPI(lang, []), "http://magpie.test/?view=providers", browser);
        try {
          for (const other of languages) {
            const missing = await page.evaluate(({ lang, keys }) => keys.filter((key) => !I18N[lang]?.[key]), { lang: other, keys: missingKeys });
            assert.deepEqual(missing, [], `${other} has every param override string`);
          }
        } finally {
          await page.close();
        }
      }
    }
  });
}
