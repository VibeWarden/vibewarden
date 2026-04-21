// Shared navigation bar + dark mode sync between landing page and docs.
// Injected into all docs HTML pages. Will need to be ported to the
// vibewarden repo's mkdocs.yml (extra_javascript) for persistence.
(function() {
  var DOCS_KEY = '/docs/vibewarden/.__palette';
  var THEME_KEY = 'theme';

  // --- Theme sync ---
  function getTheme() {
    try {
      var own = localStorage.getItem(THEME_KEY);
      if (own === 'dark' || own === 'light') return own;
      var mkdocs = JSON.parse(localStorage.getItem(DOCS_KEY));
      if (mkdocs && mkdocs.color && mkdocs.color.scheme) {
        return mkdocs.color.scheme === 'slate' ? 'dark' : 'light';
      }
    } catch(e) {}
    return window.matchMedia('(prefers-color-scheme: dark)').matches ? 'dark' : 'light';
  }

  function saveTheme(theme) {
    try {
      localStorage.setItem(THEME_KEY, theme);
      localStorage.setItem(DOCS_KEY, JSON.stringify({
        index: theme === 'dark' ? 1 : 0,
        color: { scheme: theme === 'dark' ? 'slate' : 'default', primary: 'deep-purple', accent: 'cyan', media: '' }
      }));
    } catch(e) {}
  }

  function applyTheme(theme) {
    var scheme = theme === 'dark' ? 'slate' : 'default';
    document.body.setAttribute('data-md-color-scheme', scheme);
    document.body.setAttribute('data-md-color-primary', 'deep-purple');
    document.body.setAttribute('data-md-color-accent', 'cyan');
    // Update toggle icon
    var bar = document.getElementById('vw-topbar');
    if (bar) {
      bar.querySelector('.vw-icon-sun').style.display = theme === 'dark' ? 'none' : 'block';
      bar.querySelector('.vw-icon-moon').style.display = theme === 'dark' ? 'block' : 'none';
    }
  }

  // Apply on load
  var currentTheme = getTheme();
  applyTheme(currentTheme);
  saveTheme(currentTheme);

  // Watch MkDocs palette changes and sync back
  var observer = new MutationObserver(function(mutations) {
    mutations.forEach(function(m) {
      if (m.attributeName === 'data-md-color-scheme') {
        var s = document.body.getAttribute('data-md-color-scheme');
        var t = s === 'slate' ? 'dark' : 'light';
        saveTheme(t);
        applyTheme(t);
      }
    });
  });
  observer.observe(document.body, { attributes: true, attributeFilter: ['data-md-color-scheme'] });

  // --- Inject shared top navigation bar ---
  var theme = getTheme();
  var bar = document.createElement('div');
  bar.id = 'vw-topbar';
  bar.innerHTML =
    '<div class="vw-topbar-inner">' +
      '<a href="/" class="vw-topbar-logo">' +
        '<img src="/static/logo.png" alt="" width="28" height="28">' +
        '<span>VibeWarden</span>' +
      '</a>' +
      '<nav class="vw-topbar-nav">' +
        '<a href="/">Home</a>' +
        '<a href="/blog/">Blog</a>' +
        '<a href="/docs/vibewarden/" class="vw-active">Docs</a>' +
        '<a href="/tools/">Tools</a>' +
        '<a href="/support/">Support</a>' +
        '<a href="https://github.com/vibewarden/vibewarden" rel="noopener">GitHub</a>' +
        '<button class="vw-theme-toggle" type="button" aria-label="Toggle dark mode">' +
          '<svg class="vw-icon-sun" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round" style="display:' + (theme === 'dark' ? 'none' : 'block') + '">' +
            '<circle cx="12" cy="12" r="5"/><line x1="12" y1="1" x2="12" y2="3"/><line x1="12" y1="21" x2="12" y2="23"/><line x1="4.22" y1="4.22" x2="5.64" y2="5.64"/><line x1="18.36" y1="18.36" x2="19.78" y2="19.78"/><line x1="1" y1="12" x2="3" y2="12"/><line x1="21" y1="12" x2="23" y2="12"/><line x1="4.22" y1="19.78" x2="5.64" y2="18.36"/><line x1="18.36" y1="5.64" x2="19.78" y2="4.22"/>' +
          '</svg>' +
          '<svg class="vw-icon-moon" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round" style="display:' + (theme === 'dark' ? 'block' : 'none') + '">' +
            '<path d="M21 12.79A9 9 0 1 1 11.21 3 7 7 0 0 0 21 12.79z"/>' +
          '</svg>' +
        '</button>' +
      '</nav>' +
    '</div>';

  document.body.insertBefore(bar, document.body.firstChild);

  // Toggle handler
  bar.querySelector('.vw-theme-toggle').addEventListener('click', function() {
    var next = document.body.getAttribute('data-md-color-scheme') === 'slate' ? 'light' : 'dark';
    applyTheme(next);
    saveTheme(next);
    // Also trigger MkDocs palette radio so its UI stays in sync
    var radio = document.getElementById(next === 'dark' ? '__palette_1' : '__palette_0');
    if (radio) radio.checked = true;
  });
})();
