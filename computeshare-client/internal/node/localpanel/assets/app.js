// Minimal JS: confirm destructive actions; refresh the page every 10 s when idle.
document.addEventListener("submit", function (e) {
  var b = e.submitter, m = b && b.getAttribute("data-confirm");
  if (m && !window.confirm(m)) e.preventDefault();
});
var typing = false;
document.addEventListener("input", function () { typing = true; });
setInterval(function () { if (!typing) location.reload(); }, 10000);
