// Applies the last known theme before first paint, so a reload doesn't flash the default one
// (USER-20). A plain script, because the page's policy allows no inline ones; AppearanceSync
// writes this entry and corrects it as soon as the server answers.
(function () {
  try {
    var saved = JSON.parse(localStorage.getItem("marquee.appearance"));
    if (!saved || !saved.vars) return;
    var root = document.documentElement;
    for (var name in saved.vars) root.style.setProperty(name, saved.vars[name]);
    var meta = document.querySelector('meta[name="theme-color"]');
    if (meta && saved.themeColor) meta.setAttribute("content", saved.themeColor);
  } catch (e) {
    /* storage unavailable or the entry is bad: the default theme shows until the server answers */
  }
})();
