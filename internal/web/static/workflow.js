(function () {
  var el = document.getElementById("wf-canvas");
  var hidden = document.getElementById("wf-graph");
  var form = document.getElementById("wf-form");
  var initEl = document.getElementById("wf-init");
  if (!el || !hidden || typeof Drawflow === "undefined") return;

  var graph = { nodes: [], edges: [] };
  try {
    graph = JSON.parse((initEl && initEl.value) || '{"nodes":[],"edges":[]}');
  } catch (e) {
    graph = { nodes: [], edges: [] };
  }
  if (!graph.nodes) graph.nodes = [];
  if (!graph.edges) graph.edges = [];

  function ioCounts(type) {
    if (type === "if") return { inn: 1, out: 2 };
    return { inn: 1, out: 1 };
  }

  function escapeHtml(s) {
    return String(s)
      .replace(/&/g, "&amp;")
      .replace(/</g, "&lt;")
      .replace(/>/g, "&gt;")
      .replace(/"/g, "&quot;");
  }

  function nodeHTML(type) {
    var body = "";
    if (type === "if") {
      body = '<label>match</label><input type="text" df-expr placeholder="substring in prior output">';
    } else if (type === "delay") {
      body = '<label>seconds</label><input type="number" min="0" step="1" df-seconds placeholder="0">';
    } else if (type === "foreach_server") {
      body = "<p class=\"wf-node-hint\">Runs the next step on each fleet host.</p>";
    } else {
      body = '<label>config JSON</label><textarea df-cfg rows="3" placeholder=\'{"server_id":1}\'></textarea>';
    }
    return (
      '<div class="wf-node-card">' +
      "<header>" +
      escapeHtml(type) +
      "</header>" +
      '<div class="wf-node-ports">' +
      (type === "if" ? "<span>true · false →</span>" : "<span>in → out</span>") +
      "</div>" +
      body +
      "</div>"
    );
  }

  function nodeData(n) {
    var cfg = n.config || {};
    var d = { cfg: Object.keys(cfg).length ? JSON.stringify(cfg) : "" };
    if (cfg.expr != null) d.expr = String(cfg.expr);
    if (cfg.seconds != null) d.seconds = String(cfg.seconds);
    return d;
  }

  function parseConfig(n) {
    var d = n.data || {};
    var cfg = {};
    if (d.cfg && String(d.cfg).trim()) {
      try {
        cfg = JSON.parse(d.cfg);
      } catch (e) {
        cfg = { raw: String(d.cfg) };
      }
    }
    if (d.expr != null && String(d.expr) !== "") cfg.expr = d.expr;
    if (d.seconds != null && String(d.seconds) !== "") cfg.seconds = Number(d.seconds);
    return cfg;
  }

  function toDrawflow(g) {
    var data = {};
    var idMap = {};
    var next = 1;
    (g.nodes || []).forEach(function (n) {
      var id = next++;
      idMap[String(n.id)] = String(id);
      var io = ioCounts(n.type);
      var inputs = {};
      var outputs = {};
      if (io.inn) inputs.input_1 = { connections: [] };
      if (io.out >= 1) outputs.output_1 = { connections: [] };
      if (io.out >= 2) outputs.output_2 = { connections: [] };
      data[id] = {
        id: id,
        name: n.type,
        data: nodeData(n),
        class: "wf-node" + (n.type === "if" ? " wf-if" : ""),
        html: nodeHTML(n.type),
        typenode: false,
        inputs: inputs,
        outputs: outputs,
        pos_x: Number(n.x) || 80,
        pos_y: Number(n.y) || 80,
      };
    });
    (g.edges || []).forEach(function (e) {
      var fromId = idMap[String(e.from)];
      var toId = idMap[String(e.to)];
      if (!fromId || !toId) return;
      var from = data[fromId];
      var to = data[toId];
      if (!from || !to) return;
      var outKey = from.name === "if" && e.port === "false" ? "output_2" : "output_1";
      if (!from.outputs[outKey]) from.outputs[outKey] = { connections: [] };
      from.outputs[outKey].connections.push({ node: toId, output: "input_1" });
      if (!to.inputs.input_1) to.inputs.input_1 = { connections: [] };
      to.inputs.input_1.connections.push({ node: fromId, input: outKey });
    });
    return { drawflow: { Home: { data: data } } };
  }

  function fromDrawflow(exported) {
    var data = ((((exported || {}).drawflow || {}).Home || {}).data) || {};
    var nodes = [];
    var edges = [];
    Object.keys(data).forEach(function (id) {
      var n = data[id];
      nodes.push({
        id: String(n.id),
        type: n.name,
        x: n.pos_x,
        y: n.pos_y,
        config: parseConfig(n),
      });
      var outs = n.outputs || {};
      Object.keys(outs).forEach(function (ok) {
        (outs[ok].connections || []).forEach(function (c) {
          var port = "";
          if (n.name === "if") port = ok === "output_2" ? "false" : "true";
          edges.push({ from: String(n.id), to: String(c.node), port: port });
        });
      });
    });
    return { nodes: nodes, edges: edges };
  }

  var editor = new Drawflow(el);
  editor.reroute = true;
  editor.reroute_fix_curvature = true;
  editor.force_first_input = true;
  editor.line_path = 3;
  editor.draggable_inputs = false;
  editor.zoom_max = 1.6;
  editor.zoom_min = 0.35;
  editor.zoom_value = 0.1;
  editor.start();

  function persist() {
    hidden.value = JSON.stringify(fromDrawflow(editor.export()));
  }

  ["nodeCreated", "nodeRemoved", "nodeMoved", "nodeDataChanged", "connectionCreated", "connectionRemoved", "addReroute", "removeReroute", "rerouteMoved"].forEach(function (ev) {
    editor.on(ev, persist);
  });

  if (graph.nodes.length) {
    editor.import(toDrawflow(graph));
  }
  persist();

  function canvasPos(clientX, clientY) {
    var pre = editor.precanvas;
    var zoom = editor.zoom || 1;
    var w = pre.clientWidth || 1;
    var h = pre.clientHeight || 1;
    var rect = pre.getBoundingClientRect();
    return {
      x: clientX * (w / (w * zoom)) - rect.x * (w / (w * zoom)),
      y: clientY * (h / (h * zoom)) - rect.y * (h / (h * zoom)),
    };
  }

  function addTool(type, x, y) {
    var io = ioCounts(type);
    var pos = { x: x, y: y };
    if (pos.x == null) {
      pos.x = 60 + graph.nodes.length * 24;
      pos.y = 60 + graph.nodes.length * 18;
    }
    editor.addNode(type, io.inn, io.out, pos.x, pos.y, "wf-node" + (type === "if" ? " wf-if" : ""), nodeData({ type: type, config: {} }), nodeHTML(type));
    persist();
  }

  document.querySelectorAll(".wf-tool").forEach(function (btn) {
    var type = btn.getAttribute("data-type");
    btn.setAttribute("draggable", "true");
    btn.addEventListener("dragstart", function (ev) {
      ev.dataTransfer.setData("text/plain", type);
      ev.dataTransfer.effectAllowed = "copy";
    });
    btn.addEventListener("click", function () {
      var n = Object.keys((((editor.export().drawflow || {}).Home || {}).data) || {}).length;
      addTool(type, 80 + (n % 6) * 36, 70 + (n % 8) * 28);
    });
  });

  el.addEventListener("dragover", function (ev) {
    ev.preventDefault();
    ev.dataTransfer.dropEffect = "copy";
  });
  el.addEventListener("drop", function (ev) {
    ev.preventDefault();
    var type = ev.dataTransfer.getData("text/plain");
    if (!type) return;
    var p = canvasPos(ev.clientX, ev.clientY);
    addTool(type, p.x, p.y);
  });

  function bindZoom(id, fn) {
    var b = document.getElementById(id);
    if (b) b.addEventListener("click", fn);
  }
  bindZoom("wf-zoom-in", function () { editor.zoom_in(); });
  bindZoom("wf-zoom-out", function () { editor.zoom_out(); });
  bindZoom("wf-zoom-reset", function () { editor.zoom_reset(); });

  if (form) {
    form.addEventListener("submit", persist);
  }
})();
