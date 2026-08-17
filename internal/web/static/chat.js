(function () {
  var log = document.getElementById("ai-log");
  var form = document.getElementById("ai-form");
  var input = document.getElementById("ai-input");
  var mic = document.getElementById("ai-mic");
  if (!form || !input || !log) return;

  function add(role, text) {
    var el = document.createElement("div");
    el.className = role === "you" ? "text-ink" : "text-muted";
    el.textContent = (role === "you" ? "You: " : "AI: ") + text;
    log.appendChild(el);
    log.scrollTop = log.scrollHeight;
  }

  form.addEventListener("submit", function (e) {
    e.preventDefault();
    var msg = (input.value || "").trim();
    if (!msg) return;
    add("you", msg);
    input.value = "";
    var body = new URLSearchParams();
    body.set("message", msg);
    var sel = document.getElementById("ai-server");
    if (sel) body.set("server_id", sel.value);
    fetch("/ai/chat", {
      method: "POST",
      headers: { "Content-Type": "application/x-www-form-urlencoded" },
      body: body.toString()
    }).then(function (res) {
      var reader = res.body.getReader();
      var dec = new TextDecoder();
      var buf = "";
      function pump() {
        return reader.read().then(function (chunk) {
          if (chunk.done) return;
          buf += dec.decode(chunk.value, { stream: true });
          var parts = buf.split("\n\n");
          buf = parts.pop();
          parts.forEach(function (p) {
            var line = p.replace(/^data:\s*/, "");
            if (!line) return;
            try {
              var ev = JSON.parse(line);
              if (ev.type === "text" && ev.Text) add("ai", ev.Text);
              else if (ev.type === "text" && ev.text) add("ai", ev.text);
              else if (ev.type === "tool") add("ai", "tool " + (ev.Tool || ev.tool || ""));
              else if (ev.type === "confirm") add("ai", "Confirm destructive: " + (ev.text || "") + " — type yes");
              else if (ev.type === "error") add("ai", ev.text || ev.Text || "error");
            } catch (err) {}
          });
          return pump();
        });
      }
      return pump();
    }).catch(function (err) { add("ai", String(err)); });
  });

  var rec = null;
  if (mic && navigator.mediaDevices) {
    mic.addEventListener("click", function () {
      if (rec) {
        rec.stop();
        rec = null;
        mic.textContent = "Mic";
        return;
      }
      navigator.mediaDevices.getUserMedia({ audio: true }).then(function (stream) {
        rec = new MediaRecorder(stream);
        var chunks = [];
        rec.ondataavailable = function (ev) { if (ev.data.size) chunks.push(ev.data); };
        rec.onstop = function () {
          stream.getTracks().forEach(function (t) { t.stop(); });
          var blob = new Blob(chunks, { type: rec.mimeType || "audio/webm" });
          var fd = new FormData();
          fd.append("audio", blob, "clip.webm");
          fetch("/ai/transcribe", { method: "POST", body: fd })
            .then(function (r) { return r.json(); })
            .then(function (j) { if (j.text) input.value = (input.value + " " + j.text).trim(); })
            .catch(function (err) { add("ai", String(err)); });
        };
        rec.start();
        mic.textContent = "Stop";
      }).catch(function (err) { add("ai", String(err)); });
    });
  }
})();
