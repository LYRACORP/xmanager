(function () {
  var el = document.getElementById("k8s-log");
  if (!el) return;
  var parts = location.pathname.split("/").filter(Boolean);
  var id = parts[1];
  if (!id) return;
  var es = new EventSource("/k8s/" + id + "/log");
  es.onmessage = function (ev) {
    el.textContent = ev.data;
    el.scrollTop = el.scrollHeight;
  };
  es.addEventListener("done", function () {
    es.close();
  });
})();
