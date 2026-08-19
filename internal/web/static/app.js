/* Hallmark · component: command-palette · genre: modern-minimal · theme: Cobalt
 * states: default · hover · focus · active · disabled · loading · error · success
 */
(function () {
  "use strict";

  function token(name) {
    var srgb = name.replace(/\)$/, "-srgb)");
    if (srgb === name) srgb = name + "-srgb";
    var probe = document.createElement("span");
    probe.style.color = "var(" + name + "-srgb, var(" + name + "))";
    probe.style.position = "absolute";
    probe.style.visibility = "hidden";
    document.body.appendChild(probe);
    var value = getComputedStyle(probe).color;
    probe.remove();
    if (value && value.indexOf("oklch") === -1) return value;
    var map = {
      "--color-accent": "#2f6bff",
      "--color-ok": "#1f8a5b",
      "--color-ink-2": "#5c6578",
      "--color-rule": "#d4d8e0",
      "--color-paper-2": "#f0f2f6"
    };
    return map[name] || "#2f6bff";
  }
  window.xmToken = token;

  function $(sel, root) {
    return (root || document).querySelector(sel);
  }

  function collectCommands() {
    var nodes = document.querySelectorAll("[data-cmdk]");
    var out = [];
    nodes.forEach(function (el) {
      var href = el.getAttribute("href");
      if (!href || href === "#") return;
      var label = (el.getAttribute("data-cmdk-label") || el.textContent || "").replace(/\s+/g, " ").trim();
      if (!label) return;
      out.push({ href: href, label: label });
    });
    return out;
  }

  function ensurePalette() {
    var existing = $("#xm-cmdk");
    if (existing) return existing;
    var wrap = document.createElement("div");
    wrap.id = "xm-cmdk";
    wrap.className = "xm-cmdk";
    wrap.hidden = true;
    wrap.setAttribute("role", "dialog");
    wrap.setAttribute("aria-modal", "true");
    wrap.setAttribute("aria-label", "Command palette");
    wrap.innerHTML =
      '<div class="xm-cmdk-panel">' +
      '<input type="search" id="xm-cmdk-input" placeholder="Go to…" autocomplete="off" aria-label="Filter destinations">' +
      '<div class="xm-cmdk-list" id="xm-cmdk-list" role="listbox"></div>' +
      "</div>";
    document.body.appendChild(wrap);
    return wrap;
  }

  var selected = 0;
  var filtered = [];

  function renderList() {
    var list = $("#xm-cmdk-list");
    if (!list) return;
    list.innerHTML = "";
    filtered.forEach(function (item, i) {
      var btn = document.createElement("button");
      btn.type = "button";
      btn.className = "xm-cmdk-item";
      btn.setAttribute("role", "option");
      btn.setAttribute("aria-selected", i === selected ? "true" : "false");
      btn.textContent = item.label;
      btn.addEventListener("click", function () {
        window.location.href = item.href;
      });
      list.appendChild(btn);
    });
  }

  function openPalette() {
    var cmds = collectCommands();
    if (!cmds.length) return;
    var wrap = ensurePalette();
    var input = $("#xm-cmdk-input");
    filtered = cmds.slice();
    selected = 0;
    wrap.hidden = false;
    input.value = "";
    renderList();
    input.focus();
  }

  function closePalette() {
    var wrap = $("#xm-cmdk");
    if (wrap) wrap.hidden = true;
  }

  function filterPalette(q) {
    var cmds = collectCommands();
    var needle = (q || "").toLowerCase();
    filtered = cmds.filter(function (c) {
      return c.label.toLowerCase().indexOf(needle) !== -1 || c.href.toLowerCase().indexOf(needle) !== -1;
    });
    selected = 0;
    renderList();
  }

  document.addEventListener("keydown", function (e) {
    var meta = e.metaKey || e.ctrlKey;
    if (meta && (e.key === "k" || e.key === "K")) {
      e.preventDefault();
      var wrap = $("#xm-cmdk");
      if (wrap && !wrap.hidden) closePalette();
      else openPalette();
      return;
    }
    var wrap = $("#xm-cmdk");
    if (!wrap || wrap.hidden) return;
    if (e.key === "Escape") {
      e.preventDefault();
      closePalette();
    } else if (e.key === "ArrowDown") {
      e.preventDefault();
      selected = Math.min(selected + 1, Math.max(filtered.length - 1, 0));
      renderList();
    } else if (e.key === "ArrowUp") {
      e.preventDefault();
      selected = Math.max(selected - 1, 0);
      renderList();
    } else if (e.key === "Enter") {
      e.preventDefault();
      if (filtered[selected]) window.location.href = filtered[selected].href;
    }
  });

  document.addEventListener("input", function (e) {
    if (e.target && e.target.id === "xm-cmdk-input") filterPalette(e.target.value);
  });

  document.addEventListener("click", function (e) {
    var wrap = $("#xm-cmdk");
    if (wrap && !wrap.hidden && e.target === wrap) closePalette();
    if (e.target.closest && e.target.closest("[data-open-cmdk]")) {
      e.preventDefault();
      openPalette();
    }
    if (e.target.closest && e.target.closest("[data-open-nav]")) {
      e.preventDefault();
      var app = document.querySelector(".xm-app");
      var control = document.querySelector(".xm-control-nav");
      if (app) {
        app.classList.toggle("is-nav-open");
        var back = document.querySelector(".xm-nav-backdrop");
        if (back) back.hidden = !app.classList.contains("is-nav-open");
      }
      if (control) control.classList.toggle("is-open");
    }
    if (e.target.classList && e.target.classList.contains("xm-nav-backdrop")) {
      var app = document.querySelector(".xm-app");
      if (app) app.classList.remove("is-nav-open");
      e.target.hidden = true;
    }
  });
})();
