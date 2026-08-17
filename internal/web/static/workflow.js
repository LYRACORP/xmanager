(function () {
  var svg = document.getElementById("wf-canvas");
  var hidden = document.getElementById("wf-graph");
  var form = document.getElementById("wf-form");
  var initEl = document.getElementById("wf-init");
  var graph = { nodes: [], edges: [] };
  try {
    graph = JSON.parse((initEl && initEl.value) || "{\"nodes\":[],\"edges\":[]}");
  } catch (e) {
    graph = { nodes: [], edges: [] };
  }
  if (!graph.nodes) graph.nodes = [];
  if (!graph.edges) graph.edges = [];

  var linking = null;

  function uid() {
    return "n" + Math.random().toString(36).slice(2, 8);
  }

  function render() {
    while (svg.firstChild) svg.removeChild(svg.firstChild);
    graph.edges.forEach(function (e) {
      var a = graph.nodes.find(function (n) { return n.id === e.from; });
      var b = graph.nodes.find(function (n) { return n.id === e.to; });
      if (!a || !b) return;
      var line = document.createElementNS("http://www.w3.org/2000/svg", "line");
      line.setAttribute("x1", a.x + 70);
      line.setAttribute("y1", a.y + 16);
      line.setAttribute("x2", b.x);
      line.setAttribute("y2", b.y + 16);
      line.setAttribute("stroke", "currentColor");
      line.setAttribute("stroke-width", "1.5");
      svg.appendChild(line);
    });
    graph.nodes.forEach(function (n) {
      var g = document.createElementNS("http://www.w3.org/2000/svg", "g");
      g.setAttribute("transform", "translate(" + n.x + "," + n.y + ")");
      var rect = document.createElementNS("http://www.w3.org/2000/svg", "rect");
      rect.setAttribute("width", "140");
      rect.setAttribute("height", "32");
      rect.setAttribute("rx", "6");
      rect.setAttribute("fill", "var(--color-paper)");
      rect.setAttribute("stroke", "var(--color-accent)");
      var text = document.createElementNS("http://www.w3.org/2000/svg", "text");
      text.setAttribute("x", "8");
      text.setAttribute("y", "21");
      text.setAttribute("font-size", "11");
      text.textContent = n.type;
      g.appendChild(rect);
      g.appendChild(text);
      g.addEventListener("mousedown", function (ev) {
        if (ev.shiftKey) {
          if (!linking) linking = n.id;
          else {
            graph.edges.push({ from: linking, to: n.id });
            linking = null;
            render();
          }
          return;
        }
        var startX = ev.clientX, startY = ev.clientY, ox = n.x, oy = n.y;
        function move(e2) {
          n.x = ox + (e2.clientX - startX);
          n.y = oy + (e2.clientY - startY);
          render();
        }
        function up() {
          window.removeEventListener("mousemove", move);
          window.removeEventListener("mouseup", up);
        }
        window.addEventListener("mousemove", move);
        window.addEventListener("mouseup", up);
      });
      svg.appendChild(g);
    });
    hidden.value = JSON.stringify(graph);
  }

  if (!svg || !hidden) return;

  document.querySelectorAll(".wf-tool").forEach(function (btn) {
    btn.addEventListener("click", function () {
      graph.nodes.push({
        id: uid(),
        type: btn.getAttribute("data-type"),
        x: 40 + graph.nodes.length * 12,
        y: 40 + graph.nodes.length * 12,
        config: {}
      });
      render();
    });
  });

  if (form) {
    form.addEventListener("submit", function () {
      hidden.value = JSON.stringify(graph);
    });
  }
  render();
})();
