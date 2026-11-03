// Minimal JS: confirmation for destructive actions and auto-refresh of the node list.
document.addEventListener("submit", function (e) {
  var m = e.target.getAttribute("data-confirm");
  if (m && !window.confirm(m)) e.preventDefault();
});
var r = document.querySelector("[data-autorefresh]");
if (r) setTimeout(function () { location.reload(); }, 1000 * parseInt(r.getAttribute("data-autorefresh"), 10));
