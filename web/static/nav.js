/* TrailSeal navbar v2: dropdown, mobile panel, active indicator, login -> Dashboard. */
(function () {
  var nav = document.querySelector('.tsnav');
  if (!nav) return;
  var dd = nav.querySelector('.tsnav-dd');
  var ddBtn = dd.querySelector('button');
  var burger = nav.querySelector('.tsnav-burger');
  function setDD(o) { dd.classList.toggle('open', o); ddBtn.setAttribute('aria-expanded', o); }
  function setNav(o) { nav.classList.toggle('open', o); burger.setAttribute('aria-expanded', o); document.body.style.overflow = o ? 'hidden' : ''; }
  ddBtn.addEventListener('click', function (e) { e.stopPropagation(); setDD(!dd.classList.contains('open')); });
  burger.addEventListener('click', function () { setNav(!nav.classList.contains('open')); });
  document.addEventListener('click', function (e) { if (!dd.contains(e.target)) setDD(false); });
  document.addEventListener('keydown', function (e) { if (e.key === 'Escape') { setDD(false); setNav(false); } });
  nav.querySelectorAll('.tsnav-menu a, .tsnav-panel a').forEach(function (a) {
    a.addEventListener('click', function () { setDD(false); setNav(false); });
  });

  // Active indicator: highlight "Product" while its sections are in view.
  if ('IntersectionObserver' in window) {
    var seen = {};
    var io = new IntersectionObserver(function (entries) {
      entries.forEach(function (en) { seen[en.target.id] = en.isIntersecting; });
      ddBtn.classList.toggle('is-active', Object.keys(seen).some(function (k) { return seen[k]; }));
    }, { rootMargin: '-40% 0px -50% 0px' });
    ['how-it-works', 'capabilities'].forEach(function (id) { var el = document.getElementById(id); if (el) io.observe(el); });
  }

  // Logged-in visitors: "Log in" becomes "Dashboard" (401 when logged out).
  fetch('/api/auth/session', { credentials: 'include' }).then(function (r) {
    if (!r.ok) return;
    document.querySelectorAll('[data-auth-link]').forEach(function (a) {
      a.textContent = 'Dashboard';
      a.setAttribute('href', '/dashboard');
    });
  }).catch(function () {});
})();
