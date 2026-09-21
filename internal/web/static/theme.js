// Applies the saved light/dark choice before first paint.
(function () {
  try {
    var m = localStorage.getItem('gv-mode');
    if (m === 'light' || m === 'dark') document.documentElement.dataset.mode = m;
  } catch (e) {}
})();
