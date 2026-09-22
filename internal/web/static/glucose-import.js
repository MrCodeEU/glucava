// Progressive enhancement for the "Import glucose readings" form on
// Settings: without this script, it is a plain multipart form post that
// redirects back with a flash message (works with JS off). With it, the
// submit is done via fetch and the result swapped in in place, so the page
// never reloads and you stay exactly where you were, form values and all.
(function () {
  const form = document.getElementById("glucose-import-form");
  if (!form) return;

  function onSubmit(e) {
    e.preventDefault();
    const status = document.getElementById("glucose-import-status");
    const submitBtn = form.querySelector('[type="submit"]');
    if (submitBtn) submitBtn.disabled = true;

    fetch(form.action, {
      method: "POST",
      body: new FormData(form),
      headers: { "X-Glucava-Fetch": "1" },
    })
      .then(function (r) {
        return r.text();
      })
      .then(function (html) {
        // html is our own server's rendered response (same origin, and text
        // content in it is HTML-escaped at render time by gomponents), the
        // same trust level the rest of this app already gives Datastar's
        // PatchElements; not attacker-controlled markup.
        if (status) status.outerHTML = html;
        if (form.querySelector('input[type="file"]')) {
          form.reset();
        }
      })
      .catch(function () {
        // Network error: fall back to a real submit, which still works.
        form.removeEventListener("submit", onSubmit);
        form.submit();
      })
      .finally(function () {
        if (submitBtn) submitBtn.disabled = false;
      });
  }
  form.addEventListener("submit", onSubmit);
})();
