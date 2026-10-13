// Watchtower web UI.
//
// A single page with no build step. It signs in through /ui/session and then
// reads the same /v1/* endpoints any other API client would. All page content
// is built with DOM nodes, never HTML strings, because container and image
// names come from outside.

(() => {
  "use strict";

  const CHECK_STORE = "watchtower.check";
  const REFRESH_MS = 60_000;
  const NOTICE_MS = 8_000;
  const SEARCH_THRESHOLD = 8;

  const state = {
    session: null,
    containers: [],
    containersProblem: "",
    containersLoaded: false,
    checks: new Map(),
    checkedAt: null,
    checking: false,
    updating: new Set(),
    expanded: new Set(),
    search: "",
    waitingOnly: false,
    groupByCompose: false,
    history: null,
    historyProblem: "",
    config: null,
    configProblem: "",
    settings: null,
    settingsProblem: "",
    customSchedule: false,
    customScheduleDraft: null,
  };

  const app = document.getElementById("app");
  const notices = document.getElementById("notices");

  // DOM helpers

  function el(tag, props, ...children) {
    const node = document.createElement(tag);

    for (const [key, value] of Object.entries(props || {})) {
      if (value === false || value === null || value === undefined) {
        continue;
      }

      if (key === "class") {
        node.className = value;
      } else if (key === "checked" || key === "selected" || key === "value") {
        node[key] = value;
      } else if (key.startsWith("on")) {
        node.addEventListener(key.slice(2), value);
      } else if (value === true) {
        node.setAttribute(key, "");
      } else {
        node.setAttribute(key, value);
      }
    }

    for (const child of children.flat()) {
      if (child === false || child === null || child === undefined) {
        continue;
      }

      node.append(child);
    }

    return node;
  }

  function code(text) {
    return el("code", null, text);
  }

  // Formatting

  const relative = new Intl.RelativeTimeFormat(undefined, { numeric: "auto" });
  const exact = new Intl.DateTimeFormat(undefined, { dateStyle: "medium", timeStyle: "short" });

  function ago(date) {
    const seconds = Math.round((date.getTime() - Date.now()) / 1000);
    const steps = [
      ["day", 86_400],
      ["hour", 3_600],
      ["minute", 60],
    ];

    for (const [unit, size] of steps) {
      if (Math.abs(seconds) >= size) {
        return relative.format(Math.round(seconds / size), unit);
      }
    }

    return "just now";
  }

  function count(number, singular, plural) {
    return `${number} ${number === 1 ? singular : plural || `${singular}s`}`;
  }

  function capitalise(text) {
    return text.charAt(0).toUpperCase() + text.slice(1);
  }

  function shortDigest(value) {
    if (!value) {
      return "";
    }

    const hex = value.includes(":") ? value.split(":")[1] : value;

    return hex.slice(0, 12);
  }

  function cleanName(name) {
    return String(name || "").replace(/^\//, "");
  }

  // Notices

  function notify(message, problem) {
    const close = el("button", { class: "notice-close", type: "button" }, "Dismiss");
    const notice = el("div", { class: problem ? "notice notice-problem" : "notice" }, el("span", null, message), close);
    const remove = () => notice.remove();

    close.addEventListener("click", remove);

    // A new confirmation replaces earlier ones. Problems stay until dismissed.
    if (!problem) {
      notices.querySelectorAll(".notice:not(.notice-problem)").forEach((earlier) => earlier.remove());
    }

    notices.append(notice);

    if (!problem) {
      setTimeout(remove, NOTICE_MS);
    }
  }

  // Requests

  class RequestProblem extends Error {
    constructor(message, status) {
      super(message);
      this.status = status;
    }
  }

  async function request(method, path, body) {
    let response;

    try {
      response = await fetch(path, {
        method,
        credentials: "same-origin",
        headers: body ? { "Content-Type": "application/json" } : undefined,
        body: body ? JSON.stringify(body) : undefined,
      });
    } catch {
      throw new RequestProblem("Watchtower isn't responding. Check that it's still running.", 0);
    }

    if (response.status === 401 && !path.startsWith("/ui/")) {
      state.session = { authenticated: false };
      render();

      throw new RequestProblem("Your sign-in has ended. Sign in again.", 401);
    }

    if (response.status === 429 && !path.startsWith("/v1/update")) {
      throw new RequestProblem("Too many requests in the last minute. Wait a moment and try again.", 429);
    }

    const text = await response.text();
    let data = null;

    try {
      data = text ? JSON.parse(text) : null;
    } catch {
      data = null;
    }

    if (!response.ok) {
      const reason = (data && data.error) || text || `Watchtower answered with status ${response.status}.`;

      throw new RequestProblem(reason, response.status);
    }

    return data;
  }

  // Check results survive a reload but not the browser tab.

  function restoreChecks() {
    try {
      const saved = JSON.parse(sessionStorage.getItem(CHECK_STORE));

      if (saved && Array.isArray(saved.items)) {
        state.checks = new Map(saved.items.map((item) => [cleanName(item.name), item]));
        state.checkedAt = new Date(saved.at);
      }
    } catch {
      sessionStorage.removeItem(CHECK_STORE);
    }
  }

  function saveChecks() {
    try {
      sessionStorage.setItem(
        CHECK_STORE,
        JSON.stringify({ at: state.checkedAt.toISOString(), items: [...state.checks.values()] }),
      );
    } catch {
      // Storage can be unavailable in private windows. The results still show until reload.
    }
  }

  // Data

  function feature(name) {
    return Boolean(state.session && state.session.features && state.session.features[name]);
  }

  async function loadContainers() {
    if (!feature("containers")) {
      return;
    }

    try {
      const data = await request("GET", "/v1/containers/details");

      state.containers = (data.containers || [])
        .map((item) => ({ ...item, name: cleanName(item.name) }))
        .sort((a, b) => a.name.localeCompare(b.name));
      state.containersProblem = "";
    } catch (problem) {
      state.containersProblem = problem.message;
    }

    state.containersLoaded = true;
  }

  async function loadHistory() {
    if (!feature("history")) {
      return;
    }

    try {
      const data = await request("GET", "/v1/history?limit=100");

      state.history = (data.entries || []).sort((a, b) => new Date(b.timestamp) - new Date(a.timestamp));
      state.historyProblem = "";
    } catch (problem) {
      state.historyProblem = problem.message;
    }
  }

  async function loadConfig() {
    if (!feature("config")) {
      return;
    }

    try {
      const data = await request("GET", "/v1/config");

      state.config = data.config;
      state.configProblem = "";
    } catch (problem) {
      state.configProblem = problem.message;
    }
  }

  async function loadSettings() {
    if (!feature("settings")) {
      return;
    }

    try {
      state.settings = await request("GET", "/v1/settings");
      state.settingsProblem = "";
    } catch (problem) {
      state.settingsProblem = problem.message;
    }
  }

  async function checkForUpdates() {
    if (state.checking) {
      return;
    }

    state.checking = true;
    render();

    try {
      const data = await request("POST", "/v1/check");

      state.checks = new Map((data.containers || []).map((item) => [cleanName(item.name), item]));
      state.checkedAt = new Date();
      state.history = null;
      saveChecks();
      await loadContainers();
    } catch (problem) {
      notify(`Couldn't check for updates. ${problem.message}`, true);
    }

    state.checking = false;
    render();
  }

  // The update endpoint treats each name as an anchored regular expression.
  function exactPattern(name) {
    return name.replace(/[.*+?^${}()|[\]\\]/g, "\\$&");
  }

  async function update(names) {
    const label = names.length === 1 ? names[0] : count(names.length, "container");
    const query = names.map((name) => `container=${encodeURIComponent(exactPattern(name))}`).join("&");

    names.forEach((name) => state.updating.add(name));
    render();

    try {
      const data = await request("POST", `/v1/update?${query}`);
      const summary = (data && data.summary) || {};

      if (summary.failed > 0) {
        notify(`Couldn't update ${label}. Watchtower's log has the reason.`, true);
      } else if (summary.updated > 0) {
        notify(`Updated ${label}.`);
      } else {
        notify(`${capitalise(label)} ${names.length === 1 ? "was" : "were"} already up to date.`);
      }

      if (!summary.failed) {
        for (const name of names) {
          const check = state.checks.get(name);

          if (check) {
            state.checks.set(name, { ...check, update_available: false });
          }
        }

        if (state.checkedAt) {
          saveChecks();
        }
      }
    } catch (problem) {
      const reason = problem.status === 429 ? "Another update is already running." : problem.message;

      notify(`Couldn't update ${label}. ${reason}`, true);
    }

    names.forEach((name) => state.updating.delete(name));
    await loadContainers();
    render();
  }

  // Container state, in the words the page uses for it.

  function describe(container) {
    const check = state.checks.get(container.name);

    if (state.updating.has(container.name)) {
      return { key: "working", label: "Updating" };
    }

    if (check && check.error) {
      return { key: "failed", label: "Couldn't check" };
    }

    if (check && check.update_available) {
      return { key: "waiting", label: "Update waiting" };
    }

    if (!container.running) {
      return { key: "stopped", label: "Stopped" };
    }

    if (check) {
      return { key: "fresh", label: "Up to date" };
    }

    return { key: "unknown", label: "Not checked yet" };
  }

  function canUpdate(container) {
    const globalMonitor = Boolean(state.config && state.config.monitor_only);
    const pinned = container.policy === "pinned";

    return feature("update") && !container.monitor_only && !globalMonitor && !pinned;
  }

  function waitingContainers() {
    return state.containers.filter((container) => {
      const check = state.checks.get(container.name);

      return check && check.update_available && !check.error;
    });
  }

  // Confirmation dialog

  function confirmUpdate(containers) {
    const names = containers.map((container) => container.name);
    const single = containers.length === 1;
    const dialog = el("dialog", { class: "confirm", "aria-labelledby": "confirm-title" });
    const close = () => {
      dialog.close();
      dialog.remove();
    };

    dialog.append(
      el(
        "h2",
        { class: "confirm-title", id: "confirm-title" },
        single ? `Update ${names[0]}?` : `Update ${count(names.length, "container")}?`,
      ),
      el(
        "p",
        { class: "confirm-body" },
        single
          ? "Watchtower pulls the new image, stops the container, and starts it again with the same settings. It will be unavailable for a moment."
          : `Watchtower pulls the new images, then stops and restarts each container with the same settings: ${names.join(", ")}.`,
      ),
      el(
        "div",
        { class: "confirm-actions" },
        el("button", { class: "button", type: "button", onclick: close }, "Cancel"),
        el(
          "button",
          {
            class: "button button-primary",
            type: "button",
            onclick: () => {
              close();
              update(names);
            },
          },
          single ? "Update container" : `Update ${count(names.length, "container")}`,
        ),
      ),
    );

    dialog.addEventListener("cancel", () => dialog.remove());
    document.body.append(dialog);
    dialog.showModal();
  }

  // Containers view

  function lookout() {
    const total = state.containers.length;
    const waiting = waitingContainers();
    const unchecked = [...state.checks.values()].filter((check) => check.error).length;
    const updatable = waiting.filter(canUpdate);
    let line;
    let note;

    if (state.containersProblem && total === 0) {
      line = "The container list didn't load.";
      note = state.containersProblem;
    } else if (!state.containersLoaded) {
      line = "Looking over the yard.";
      note = "Loading the containers Watchtower is watching.";
    } else if (total === 0) {
      line = "No containers on watch.";
      note = "Nothing matches Watchtower's current filter. Settings shows which containers it's set to watch.";
    } else if (!state.checkedAt) {
      line = `${capitalise(count(total, "container"))} on watch.`;
      note = feature("check")
        ? "Check for updates to see which ones have a newer image waiting."
        : "Watchtower is updating these on its own schedule.";
    } else if (waiting.length === 0) {
      line = total === 1 ? "Your container is up to date." : `All ${total} containers are up to date.`;
      note = `Checked ${ago(state.checkedAt)}.`;
    } else {
      line = `${waiting.length} of ${count(total, "container")} ${waiting.length === 1 ? "has" : "have"} an update waiting.`;
      note = `Checked ${ago(state.checkedAt)}.`;
    }

    if (state.checkedAt && unchecked > 0) {
      note += ` ${capitalise(count(unchecked, "container"))} couldn't be checked.`;
    }

    return el(
      "header",
      { class: "lookout" },
      el("h1", { class: "lookout-line" }, line),
      el("p", { class: "lookout-note" }, note),
      el(
        "div",
        { class: "actions" },
        feature("check") &&
          total > 0 &&
          el(
            "button",
            {
              class: updatable.length > 0 ? "button" : "button button-primary",
              type: "button",
              disabled: state.checking,
              onclick: checkForUpdates,
            },
            state.checking ? "Checking for updates" : "Check for updates",
          ),
        updatable.length > 1 &&
          el(
            "button",
            {
              class: "button button-primary",
              type: "button",
              disabled: state.updating.size > 0,
              onclick: () => confirmUpdate(updatable),
            },
            `Update all ${updatable.length}`,
          ),
      ),
    );
  }

  function facts(container) {
    const check = state.checks.get(container.name) || {};
    const rows = [];
    const add = (label, value) => {
      if (value) {
        rows.push(el("dt", null, label), el("dd", null, value));
      }
    };
    const link = (href) => (/^https?:\/\//.test(href) ? el("a", { class: "link-subtle", href, rel: "noreferrer", target: "_blank" }, href) : href);

    add("Image", code(container.image));

    if (check.current_image_version || check.latest_image_version) {
      add(
        "Version",
        check.update_available && check.latest_image_version
          ? `${check.current_image_version || "unknown"}, with ${check.latest_image_version} waiting`
          : check.current_image_version,
      );
    }

    add("Running digest", container.digest && code(shortDigest(container.digest)));
    add("Newest digest", check.update_available && check.latest_digest && code(shortDigest(check.latest_digest)));
    add("Image ID", container.image_id && code(shortDigest(container.image_id)));
    if (container.compose_project) {
      add("Compose stack", `${container.compose_project}${container.compose_service ? ` (${container.compose_service})` : ""}`);
    }
    if (container.health) {
      add("Health", container.health);
    }
    const sourceLink = container.source || check.oci_source;
    if (sourceLink) {
      add("Release notes / Source", link(sourceLink));
    }
    add("Changes", check.changelog && link(check.changelog));
    add("Git", check.git_repo && `${check.git_repo}${check.git_ref ? ` at ${check.git_ref}` : ""}`);
    add("Scope", container.scope);

    if (feature("settings")) {
      const currentPolicy = container.policy || "auto";
      const policySelect = el(
        "select",
        {
          class: "policy-select",
          value: currentPolicy,
          onchange: async (event) => {
            const nextPolicy = event.target.value;
            try {
              await changeSettings({
                container_overrides: {
                  [container.name]: nextPolicy,
                },
              });
              await loadContainers();
              renderManifest();
            } catch (err) {
              notify(`Failed to update policy: ${err.message}`, true);
            }
          },
        },
        el("option", { value: "auto", selected: currentPolicy === "auto" }, "Auto (Global settings)"),
        el("option", { value: "update", selected: currentPolicy === "update" }, "Always update"),
        el("option", { value: "monitor_only", selected: currentPolicy === "monitor_only" }, "Monitor only"),
        el("option", { value: "pinned", selected: currentPolicy === "pinned" }, "Pinned (Lock image)"),
      );
      add("Update policy", policySelect);
    }

    const notes = [];

    if (container.watchtower) {
      notes.push("This is Watchtower itself.");
    }

    if (container.policy === "pinned") {
      notes.push("Pinned: This container is locked to its current image and will not be updated.");
    } else if (container.monitor_only) {
      notes.push("Monitor only: Watchtower reports updates for this container but doesn't apply them.");
    }

    if (container.no_pull) {
      notes.push("Watchtower doesn't pull new images for this container.");
    }

    if (!container.running) {
      notes.push("The container isn't running.");
    }

    return el(
      "div",
      { class: "unit-detail", id: `detail-${container.name}` },
      check.error && el("p", { class: "problem" }, `Watchtower couldn't check this image: ${check.error}`),
      el("dl", { class: "facts" }, rows),
      notes.map((text) => el("p", null, text)),
    );
  }

  function unit(container) {
    const status = describe(container);
    const open = state.expanded.has(container.name);
    const check = state.checks.get(container.name);
    const waiting = status.key === "waiting";
    const health = container.health ? String(container.health).toLowerCase() : "";

    const toggle = el(
      "button",
      {
        class: "unit-toggle",
        type: "button",
        "aria-expanded": String(open),
        "aria-controls": `detail-${container.name}`,
        onclick: () => {
          if (open) {
            state.expanded.delete(container.name);
          } else {
            state.expanded.add(container.name);
          }

          renderManifest();
        },
      },
      el("span", { class: `end end-${status.key}`, "aria-hidden": "true" }),
      el(
        "span",
        null,
        el(
          "span",
          { class: "unit-name" },
          container.name,
          health && el("span", { class: `badge-health health-${health}` }, health),
        ),
        el("span", { class: "unit-image" }, container.image),
      ),
      el("span", { class: `unit-state state-${status.key}` }, status.label),
    );

    let action = null;

    if (container.policy === "pinned") {
      action = el("span", { class: "setting-off" }, "Pinned");
    } else if (waiting && canUpdate(container)) {
      action = el(
        "button",
        {
          class: "button button-quiet",
          type: "button",
          disabled: state.updating.size > 0,
          "aria-label": `Update ${container.name}`,
          onclick: () => confirmUpdate([container]),
        },
        "Update",
      );
    } else if (waiting && (check || container.monitor_only) && feature("update")) {
      action = el("span", { class: "setting-off" }, "Monitor only");
    }

    return el(
      "li",
      { class: "unit" },
      el("div", { class: "unit-head" }, toggle, el("div", { class: "unit-action" }, action)),
      open && facts(container),
    );
  }

  function visibleContainers() {
    const term = state.search.trim().toLowerCase();

    return state.containers.filter((container) => {
      if (state.waitingOnly) {
        const check = state.checks.get(container.name);

        if (!check || !check.update_available) {
          return false;
        }
      }

      return !term || container.name.toLowerCase().includes(term) || container.image.toLowerCase().includes(term);
    });
  }

  function manifest() {
    if (state.containersProblem) {
      return el(
        "p",
        { class: "blank" },
        el("strong", null, "The container list didn't load."),
        state.containersProblem,
      );
    }

    const shown = visibleContainers();

    if (state.containers.length > 0 && shown.length === 0) {
      return el(
        "p",
        { class: "blank" },
        el("strong", null, "Nothing matches."),
        state.waitingOnly ? "No container in this view has an update waiting." : "Try a shorter name or part of the image.",
      );
    }

    if (state.groupByCompose) {
      const groups = new Map();

      for (const item of shown) {
        const project = item.compose_project || "Standalone Containers";

        if (!groups.has(project)) {
          groups.set(project, []);
        }

        groups.get(project).push(item);
      }

      const sortedProjects = [...groups.keys()].sort((a, b) => {
        if (a === "Standalone Containers") {
          return 1;
        }

        if (b === "Standalone Containers") {
          return -1;
        }

        return a.localeCompare(b);
      });

      return el(
        "div",
        { id: "manifest" },
        sortedProjects.map((project) => {
          const projectContainers = groups.get(project);
          const updatableInStack = projectContainers.filter((c) => {
            const status = describe(c);

            return status.key === "waiting" && canUpdate(c);
          });

          return el(
            "div",
            { class: "stack-group" },
            el(
              "div",
              { class: "stack-header" },
              el("span", { class: "stack-title" }, project),
              el(
                "div",
                { class: "stack-meta" },
                el("span", { class: "setting-help" }, count(projectContainers.length, "container")),
                updatableInStack.length > 0 &&
                  el(
                    "button",
                    {
                      class: "button button-quiet button-sm",
                      type: "button",
                      disabled: state.updating.size > 0,
                      onclick: () => confirmUpdate(updatableInStack),
                    },
                    `Update Stack (${updatableInStack.length})`,
                  ),
              ),
            ),
            el("ul", { class: "manifest" }, projectContainers.map(unit)),
          );
        }),
      );
    }

    return el("ul", { class: "manifest", id: "manifest" }, shown.map(unit));
  }

  function renderManifest() {
    const current = document.getElementById("manifest-slot");

    if (current) {
      current.replaceChildren(manifest());
    }
  }

  function containersView() {
    if (!feature("containers")) {
      return el(
        "div",
        null,
        el("h1", { class: "title" }, "The container list is switched off."),
        el(
          "p",
          { class: "lede" },
          "Add ",
          code("containers"),
          " to ",
          code("WATCHTOWER_HTTP_API_ENDPOINTS"),
          " and restart Watchtower to see what it's watching.",
        ),
      );
    }

    const hasCompose = state.containers.some((c) => c.compose_project);
    const toolbar =
      state.containers.length > SEARCH_THRESHOLD || state.search || state.waitingOnly || hasCompose
        ? el(
            "div",
            { class: "toolbar" },
            el("input", {
              class: "search",
              type: "search",
              placeholder: "Find a container or image",
              "aria-label": "Find a container or image",
              value: state.search,
              oninput: (event) => {
                state.search = event.target.value;
                renderManifest();
              },
            }),
            hasCompose &&
              el(
                "label",
                { class: "toggle" },
                el("input", {
                  type: "checkbox",
                  checked: state.groupByCompose,
                  onchange: (event) => {
                    state.groupByCompose = event.target.checked;
                    renderManifest();
                  },
                }),
                "Group by Compose stack",
              ),
            state.checkedAt &&
              el(
                "label",
                { class: "toggle" },
                el("input", {
                  type: "checkbox",
                  checked: state.waitingOnly,
                  onchange: (event) => {
                    state.waitingOnly = event.target.checked;
                    renderManifest();
                  },
                }),
                "Only updates waiting",
              ),
          )
        : null;

    return el(
      "div",
      null,
      lookout(),
      toolbar,
      state.containersLoaded && state.containers.length > 0 && el("div", { id: "manifest-slot" }, manifest()),
    );
  }

  // History view

  function scan(entry) {
    const when = new Date(entry.timestamp);
    const scanned = entry.scanned || 0;
    const check = entry.kind === "check";
    const waiting = entry.updates_available || 0;
    const parts = [count(scanned, "container") + (check ? " checked" : " scanned")];

    if (check) {
      if (waiting) {
        parts.push(`${count(waiting, "update")} waiting`);
      } else if (!entry.failed) {
        parts.push("all up to date");
      }
    }

    if (entry.updated) {
      parts.push(`${entry.updated} updated`);
    }

    if (entry.failed) {
      parts.push(`${entry.failed} failed`);
    }

    if (entry.restarted) {
      parts.push(`${entry.restarted} restarted`);
    }

    if (entry.skipped) {
      parts.push(`${entry.skipped} skipped`);
    }

    const bar = el("div", { class: "scan-bar", "aria-hidden": "true" });
    const segment = (kind, value) => {
      if (value > 0 && scanned > 0) {
        const part = el("span", { class: `scan-part-${kind}` });

        part.style.setProperty("flex", `${value} 0 0`);
        bar.append(part);
      }
    };

    const changed = check ? waiting : entry.updated || 0;

    segment("updated", changed);
    segment("failed", entry.failed || 0);
    segment("fresh", Math.max(scanned - changed - (entry.failed || 0), 0));

    return el(
      "li",
      { class: "scan" },
      el("div", { class: "scan-when" }, capitalise(ago(when)), el("span", { class: "scan-exact" }, exact.format(when))),
      el("div", null, parts.join(", "), bar),
    );
  }

  function historyView() {
    const header = [el("h1", { class: "title" }, "Scan history")];

    if (!feature("history")) {
      return el(
        "div",
        null,
        header,
        el(
          "p",
          { class: "lede" },
          "History is switched off. Add ",
          code("history"),
          " to ",
          code("WATCHTOWER_HTTP_API_ENDPOINTS"),
          " and restart Watchtower.",
        ),
      );
    }

    header.push(
      el(
        "p",
        { class: "lede" },
        "Every time Watchtower looks for new images, whether on its schedule or when you check or update from this page. This log is kept in memory, so it starts again when Watchtower restarts.",
      ),
    );

    if (state.historyProblem) {
      return el(
        "div",
        null,
        header,
        el("p", { class: "blank" }, el("strong", null, "History didn't load."), state.historyProblem),
      );
    }

    if (!state.history) {
      return el("div", null, header, el("p", { class: "blank" }, "Loading scans."));
    }

    if (state.history.length === 0) {
      return el(
        "div",
        null,
        header,
        el(
          "p",
          { class: "blank" },
          el("strong", null, "No scans yet."),
          "The first one appears here after you check for updates, update a container, or Watchtower's next scheduled run.",
        ),
      );
    }

    return el("div", null, header, el("ul", { class: "manifest" }, state.history.map(scan)));
  }

  // Settings view

  const SCHEDULES = [
    { label: "Every hour", seconds: 3_600 },
    { label: "Every 6 hours", seconds: 21_600 },
    { label: "Every 12 hours", seconds: 43_200 },
    { label: "Every day", seconds: 86_400 },
    { label: "Every week", seconds: 604_800 },
  ];

  // Reads the length of an "@every" schedule in seconds, or null for a cron expression.
  function everySeconds(spec) {
    const match = /^@every\s+(.+)$/.exec(spec || "");

    if (!match) {
      return null;
    }

    const units = { h: 3_600, m: 60, s: 1 };
    let total = 0;
    let rest = match[1].trim();

    while (rest) {
      const part = /^(\d+)([hms])/.exec(rest);

      if (!part) {
        return null;
      }

      total += Number(part[1]) * units[part[2]];
      rest = rest.slice(part[0].length);
    }

    return total;
  }

  function scheduleChoice(spec) {
    if (!spec) {
      return "off";
    }

    const seconds = everySeconds(spec);
    const preset = SCHEDULES.find((item) => item.seconds === seconds);

    return preset ? String(preset.seconds) : "custom";
  }

  async function changeSettings(changes, focusId) {
    try {
      state.settings = await request("PATCH", "/v1/settings", changes);
      state.customSchedule = false;
      state.customScheduleDraft = null;
      await loadConfig();
      notify(state.settings.saved ? "Settings saved." : "Settings changed until Watchtower restarts.");
    } catch (problem) {
      notify(`That change wasn't made. ${capitalise(problem.message)}.`, true);
    }

    render();

    const focused = focusId && document.getElementById(focusId);

    if (focused) {
      focused.focus();
    }
  }

  function setting(name, on, help) {
    return el(
      "li",
      { class: "setting" },
      el("span", { class: "setting-name" }, name),
      el("span", { class: on ? "setting-value" : "setting-value setting-off" }, on ? "On" : "Off"),
      el("span", { class: "setting-help" }, help),
    );
  }

  // A setting that can be switched from this page. `inverted` is for settings
  // whose stored value is the opposite of how the page words them.
  function switchSetting(key, name, help, inverted) {
    const entry = state.settings.settings[key];
    const on = inverted ? !entry.value : entry.value;
    const id = `setting-${key}`;

    return el(
      "li",
      { class: "setting" },
      el("label", { class: "setting-name", for: id }, name),
      el(
        "span",
        { class: "setting-value" },
        el("input", {
          class: "switch",
          id,
          type: "checkbox",
          role: "switch",
          checked: on,
          onchange: (event) => changeSettings({ [key]: inverted ? !event.target.checked : event.target.checked }, id),
        }),
      ),
      el(
        "span",
        { class: "setting-help" },
        help,
        entry.changed && [
          " Changed on this page. ",
          el(
            "button",
            { class: "link-button", type: "button", onclick: () => changeSettings({ [key]: null }, id) },
            "Go back to the startup value",
          ),
        ],
      ),
    );
  }

  function scheduleSetting() {
    const entry = state.settings.settings.schedule;
    const choice = state.customSchedule ? "custom" : scheduleChoice(entry.value);
    const selectId = "setting-schedule";
    const customId = "setting-schedule-custom";

    const select = el(
      "select",
      {
        class: "select",
        id: selectId,
        onchange: (event) => {
          const picked = event.target.value;

          if (picked === "custom") {
            state.customSchedule = true;
            render();
            document.getElementById(customId).focus();
          } else {
            changeSettings({ schedule: picked === "off" ? "" : `@every ${Number(picked) / 3_600}h` }, selectId);
          }
        },
      },
      SCHEDULES.map((item) =>
        el("option", { value: String(item.seconds), selected: choice === String(item.seconds) }, item.label),
      ),
      el("option", { value: "custom", selected: choice === "custom" }, "A custom schedule"),
      el("option", { value: "off", selected: choice === "off" }, "Never"),
    );

    const custom =
      choice === "custom" &&
      el(
        "form",
        {
          class: "inline-form",
          onsubmit: (event) => {
            event.preventDefault();
            // Kept so a rejected schedule stays in the field to be corrected.
            state.customScheduleDraft = document.getElementById(customId).value;
            changeSettings({ schedule: state.customScheduleDraft }, customId);
          },
        },
        el("input", {
          class: "search",
          id: customId,
          type: "text",
          spellcheck: "false",
          autocomplete: "off",
          "aria-label": "Custom schedule",
          value: state.customScheduleDraft === null ? entry.value : state.customScheduleDraft,
        }),
        el("button", { class: "button button-quiet", type: "submit" }, "Save schedule"),
      );

    return el(
      "li",
      { class: "setting setting-wide" },
      el("label", { class: "setting-name", for: selectId }, "Look for updates"),
      el("span", { class: "setting-value" }, select),
      el(
        "span",
        { class: "setting-help" },
        state.settings.schedule_active
          ? "How often Watchtower checks every container on its own. The first check comes one interval after you change this."
          : [
              "Scheduled checks are off because the update endpoint is on without ",
              code("WATCHTOWER_HTTP_API_PERIODIC_POLLS=true"),
              ". Watchtower only updates when asked, so this has no effect yet.",
            ],
        choice === "custom" && [
          " A custom schedule is a cron expression with seconds first, such as ",
          code("0 0 4 * * *"),
          " for every day at 4:00.",
        ],
        entry.changed && [
          " Changed on this page. ",
          el(
            "button",
            { class: "link-button", type: "button", onclick: () => changeSettings({ schedule: null }, selectId) },
            "Go back to the startup value",
          ),
        ],
      ),
      custom,
    );
  }

  function enableHint(endpoint, extra) {
    return ["To turn it on, add ", code(endpoint), " to ", code("WATCHTOWER_HTTP_API_ENDPOINTS"), ".", extra || ""];
  }

  function settingsView() {
    const config = state.config;
    const editable = feature("settings") && state.settings;
    const sections = [
      el("h1", { class: "title" }, "Settings"),
      el(
        "p",
        { class: "lede" },
        editable
          ? "Switches and the schedule can be changed here and take effect straight away. Everything else comes from Watchtower's flags and environment variables."
          : "How this Watchtower is set up. These come from its flags and environment variables, so change them there and restart.",
      ),
    ];

    if (editable && !state.settings.saved) {
      sections.push(
        el(
          "p",
          { class: "problem" },
          "Changes made here last until Watchtower restarts. To keep them, mount a folder at ",
          code("/data"),
          " in the container, for example ",
          code("./watchtower-data:/data"),
          ".",
        ),
      );
    }

    if (feature("settings") && state.settingsProblem) {
      sections.push(
        el("p", { class: "blank" }, el("strong", null, "The editable settings didn't load."), state.settingsProblem),
      );
    }

    if (editable) {
      sections.push(
        el(
          "section",
          { class: "section" },
          el("h2", { class: "section-title" }, "Schedule"),
          el("ul", { class: "manifest" }, scheduleSetting()),
        ),
      );
    }

    if (editable || (feature("config") && config)) {
      const rows = [];

      if (editable) {
        rows.push(
          switchSetting(
            "monitor_only",
            "Apply updates",
            "When off, Watchtower reports new images but leaves containers alone.",
            true,
          ),
          switchSetting("cleanup", "Remove old images", "Delete the previous image once a container is running the new one."),
          switchSetting(
            "rolling_restart",
            "Rolling restart",
            "Restart containers one at a time instead of all together. Needs Apply updates to be on.",
          ),
          switchSetting(
            "auto_rollback",
            "Automatic rollback",
            "Revert containers to their previous image if a new image fails to start or crashes.",
          ),
        );
      } else {
        rows.push(
          setting(
            "Apply updates",
            !config.monitor_only,
            config.monitor_only
              ? "Monitor only. Watchtower reports new images but leaves containers alone."
              : "Watchtower replaces a container when its image has a newer version.",
          ),
          setting("Remove old images", config.cleanup, "Delete the previous image once a container is running the new one."),
          setting("Rolling restart", config.rolling_restart, "Restart containers one at a time instead of all together."),
        );
      }

      if (config) {
        rows.push(
          setting("Pull new images", !config.no_pull, "Fetch images from the registry before comparing them."),
          setting("Restart containers", !config.no_restart, "Restart a container after its image is updated."),
          setting("Lifecycle hooks", config.lifecycle_hooks, "Run the commands set in container labels before and after an update."),
          setting("Git watching", config.git_enable, "Watch Git repositories as well as image registries."),
        );
      }

      sections.push(
        el(
          "section",
          { class: "section" },
          el("h2", { class: "section-title" }, "Updating"),
          el("ul", { class: "manifest" }, rows),
        ),
      );

      if (editable) {
        sections.push(
          el(
            "section",
            { class: "section" },
            el("h2", { class: "section-title" }, "Notifications"),
            el(
              "ul",
              { class: "manifest" },
              el(
                "li",
                { class: "setting" },
                el("span", { class: "setting-name" }, "Test notifications"),
                el(
                  "span",
                  { class: "setting-value" },
                  el(
                    "button",
                    {
                      class: "button button-quiet",
                      type: "button",
                      onclick: async (event) => {
                        const btn = event.target;
                        btn.disabled = true;
                        btn.textContent = "Sending...";
                        try {
                          const res = await request("POST", "/v1/settings/test-notification", {});
                          notify(res && res.message ? res.message : "Test notification sent successfully!");
                        } catch (err) {
                          notify(`Test notification failed: ${err.message}`, true);
                        } finally {
                          btn.disabled = false;
                          btn.textContent = "Send test notification";
                        }
                      },
                    },
                    "Send test notification",
                  ),
                ),
                el("span", { class: "setting-help" }, "Dispatches a test alert through configured Shoutrrr notification URLs."),
              ),
            ),
          ),
        );
      }
    }

    if (feature("config") && config) {
      sections.push(
        el(
          "section",
          { class: "section" },
          el("h2", { class: "section-title" }, "Which containers"),
          el(
            "ul",
            { class: "manifest" },
            el(
              "li",
              { class: "setting" },
              el("span", { class: "setting-name" }, "Watching"),
              el("span", { class: "setting-help" }, config.filter_desc || "All containers"),
            ),
            setting("Only labelled containers", config.label_enable, [
              "Watch only containers with the label ",
              code("com.centurylinklabs.watchtower.enable=true"),
              ".",
            ]),
            setting("Stopped containers", config.include_stopped, "Include containers that aren't running."),
            setting("Restarting containers", config.include_restarting, "Include containers that are restarting."),
            config.scope &&
              el(
                "li",
                { class: "setting" },
                el("span", { class: "setting-name" }, "Scope"),
                el("span", { class: "setting-help" }, code(config.scope)),
              ),
          ),
        ),
      );
    } else if (feature("config") && state.configProblem) {
      sections.push(el("p", { class: "blank" }, el("strong", null, "Settings didn't load."), state.configProblem));
    } else if (!feature("config")) {
      sections.push(
        el(
          "p",
          { class: "lede" },
          "Watchtower isn't sharing the rest of its settings. Add ",
          code("config"),
          " to ",
          code("WATCHTOWER_HTTP_API_ENDPOINTS"),
          " to see them here.",
        ),
      );
    }

    sections.push(
      el(
        "section",
        { class: "section" },
        el("h2", { class: "section-title" }, "What this page can do"),
        el(
          "ul",
          { class: "manifest" },
          setting(
            "List containers",
            feature("containers"),
            feature("containers") ? "Shows the containers Watchtower is watching." : enableHint("containers"),
          ),
          setting(
            "Check for updates",
            feature("check"),
            feature("check") ? "Looks for newer images without changing anything." : enableHint("check"),
          ),
          setting(
            "Update containers",
            feature("update"),
            feature("update")
              ? "Updates a container when you ask."
              : enableHint("update", [
                  " Also set ",
                  code("WATCHTOWER_HTTP_API_PERIODIC_POLLS=true"),
                  ", or Watchtower stops updating on its own schedule.",
                ]),
          ),
          setting(
            "Scan history",
            feature("history"),
            feature("history") ? "Lists Watchtower's recent scans." : enableHint("history"),
          ),
          setting("Show settings", feature("config"), feature("config") ? "Shows the setup above." : enableHint("config")),
          setting(
            "Change settings",
            feature("settings"),
            feature("settings") ? "Lets you change the schedule and switches above." : enableHint("settings"),
          ),
        ),
      ),
    );

    return el("div", null, sections);
  }

  // Sign in

  function gate() {
    const input = el("input", {
      class: "field-input",
      id: "token",
      type: "password",
      autocomplete: "current-password",
      required: true,
      "aria-describedby": "token-help",
    });
    const error = el("p", { class: "field-error", role: "alert" });
    const submit = el("button", { class: "button button-primary", type: "submit" }, "Sign in");

    const form = el(
      "form",
      {
        class: "form",
        onsubmit: async (event) => {
          event.preventDefault();
          submit.disabled = true;
          error.textContent = "";

          try {
            state.session = await request("POST", "/ui/session", { token: input.value });
            await start();
          } catch (problem) {
            error.textContent =
              problem.status === 401
                ? "That token doesn't match. Check WATCHTOWER_HTTP_API_TOKEN and try again."
                : problem.message;
            submit.disabled = false;
            input.select();
          }
        },
      },
      el(
        "div",
        { class: "field" },
        el("label", { class: "field-label", for: "token" }, "API token"),
        input,
        el(
          "p",
          { class: "field-help", id: "token-help" },
          "The value you set for ",
          code("WATCHTOWER_HTTP_API_TOKEN"),
          ". This browser stays signed in for 30 days.",
        ),
      ),
      error,
      submit,
    );

    queueMicrotask(() => input.focus());

    return el(
      "main",
      { class: "gate", id: "main" },
      el(
        "div",
        { class: "gate-ends", "aria-hidden": "true" },
        el("span", { class: "end end-fresh" }),
        el("span", { class: "end end-fresh" }),
        el("span", { class: "end end-waiting" }),
        el("span", { class: "end end-fresh" }),
      ),
      el("h1", { class: "title" }, "Watchtower"),
      form,
    );
  }

  // Frame and routing

  const views = [
    { hash: "#/containers", label: "Containers", draw: containersView },
    { hash: "#/history", label: "History", draw: historyView },
    { hash: "#/settings", label: "Settings", draw: settingsView },
  ];

  function currentView() {
    return views.find((view) => view.hash === location.hash) || views[0];
  }

  async function signOut() {
    try {
      await request("DELETE", "/ui/session");
    } catch {
      // The cookie may already be gone. Showing the sign-in page is right either way.
    }

    sessionStorage.removeItem(CHECK_STORE);
    state.session = { authenticated: false };
    state.checks = new Map();
    state.checkedAt = null;
    render();
  }

  function render() {
    if (!state.session) {
      app.replaceChildren();

      return;
    }

    if (!state.session.authenticated) {
      app.replaceChildren(gate());

      return;
    }

    const view = currentView();

    document.title = `${view.label} | Watchtower`;

    app.replaceChildren(
      el(
        "header",
        { class: "masthead" },
        el(
          "a",
          { class: "wordmark", href: views[0].hash },
          el("span", { class: "end end-small end-fresh", "aria-hidden": "true" }),
          "Watchtower",
        ),
        el(
          "nav",
          { class: "nav", "aria-label": "Sections" },
          views.map((item) =>
            el("a", { class: "nav-link", href: item.hash, "aria-current": item === view && "page" }, item.label),
          ),
        ),
        el("button", { class: "link-button", type: "button", onclick: signOut }, "Sign out"),
      ),
      el("main", { class: "page", id: "main" }, view.draw()),
      el(
        "footer",
        { class: "foot" },
        state.session.version ? `Watchtower ${state.session.version}` : "Watchtower",
        feature("swagger") && [". ", el("a", { href: "/swagger/index.html" }, "API reference")],
      ),
    );
  }

  async function refresh() {
    if (!state.session || !state.session.authenticated || document.hidden) {
      return;
    }

    if (state.updating.size > 0 || state.checking || document.querySelector("dialog[open]")) {
      return;
    }

    const view = currentView();

    if (view.draw === historyView) {
      await loadHistory();
      render();
    } else if (view.draw === containersView) {
      await loadContainers();
      renderManifest();
    }
  }

  async function start() {
    restoreChecks();
    render();
    await Promise.all([loadContainers(), loadConfig(), loadSettings()]);
    render();

    if (currentView().draw === historyView) {
      await loadHistory();
      render();
    }
  }

  window.addEventListener("hashchange", async () => {
    render();
    window.scrollTo(0, 0);

    if (currentView().draw === historyView) {
      await loadHistory();
      render();
    }
  });

  (async () => {
    try {
      state.session = await request("GET", "/ui/session");
    } catch (problem) {
      state.session = { authenticated: false };
      notify(problem.message, true);
    }

    if (state.session.authenticated) {
      await start();
    } else {
      render();
    }

    setInterval(refresh, REFRESH_MS);
  })();
})();
