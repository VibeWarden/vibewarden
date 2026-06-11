/* VibeWarden admin console — vanilla JS, CSP-safe (no inline code, no eval).
   Token lives in sessionStorage only; every API call sends X-Admin-Key.
   Any 401 clears the token and returns to the gate. */

(function () {
  "use strict";

  var TOKEN_KEY = "vibewarden_admin_token";
  var API_USERS = "/_vibewarden/admin/users";
  var PER_PAGE = 20;

  var state = {
    page: 1,
    total: 0,
    perPage: PER_PAGE,
    users: [],
    confirmTimer: null
  };

  // ── dom ────────────────────────────────────────────────────
  function $(id) { return document.getElementById(id); }

  var viewGate = $("view-gate");
  var viewApp = $("view-app");
  var gateForm = $("gate-form");
  var gateToken = $("gate-token");
  var gateError = $("gate-error");

  var stLoading = $("state-loading");
  var stError = $("state-error");
  var stErrorMsg = $("state-error-msg");
  var stEmpty = $("state-empty");
  var stTable = $("state-table");
  var tbody = $("users-tbody");

  var countChip = $("user-count");
  var countNum = $("user-count-num");
  var pagerPrev = $("pager-prev");
  var pagerNext = $("pager-next");
  var pagerLabel = $("pager-label");

  var backdrop = $("modal-backdrop");
  var modal = $("modal");
  var createForm = $("create-form");
  var createEmail = $("create-email");
  var createError = $("create-error");
  var createSubmit = $("create-submit");
  var createSuccess = $("create-success");
  var createdEmail = $("created-email");
  var recoveryLink = $("recovery-link");

  var toastEl = $("toast");
  var toastTimer = null;

  // ── helpers ────────────────────────────────────────────────
  function token() { return sessionStorage.getItem(TOKEN_KEY) || ""; }

  function show(el) { el.classList.remove("hidden"); }
  function hide(el) { el.classList.add("hidden"); }

  function toast(msg) {
    toastEl.textContent = msg;
    show(toastEl);
    clearTimeout(toastTimer);
    toastTimer = setTimeout(function () { hide(toastEl); }, 2400);
  }

  function copyText(text, doneMsg) {
    function fallback() {
      var ta = document.createElement("textarea");
      ta.value = text;
      ta.setAttribute("readonly", "");
      ta.className = "visually-hidden";
      document.body.appendChild(ta);
      ta.select();
      try { document.execCommand("copy"); toast(doneMsg); }
      catch (e) { toast("Copy failed — select it manually"); }
      document.body.removeChild(ta);
    }
    if (navigator.clipboard && navigator.clipboard.writeText) {
      navigator.clipboard.writeText(text).then(
        function () { toast(doneMsg); },
        fallback
      );
    } else {
      fallback();
    }
  }

  function humanize(iso) {
    if (!iso) return "—";
    var d = new Date(iso);
    if (isNaN(d.getTime())) return iso;
    var diff = Date.now() - d.getTime();
    var day = 86400000;
    if (diff < 60000) return "just now";
    if (diff < 3600000) return Math.floor(diff / 60000) + "m ago";
    if (diff < day) return Math.floor(diff / 3600000) + "h ago";
    if (diff < 7 * day) return Math.floor(diff / day) + "d ago";
    return d.toLocaleDateString(undefined, { year: "numeric", month: "short", day: "numeric" });
  }

  // ── api ────────────────────────────────────────────────────
  function api(method, path, body) {
    var opts = {
      method: method,
      headers: { "X-Admin-Key": token() }
    };
    if (body !== undefined) {
      opts.headers["Content-Type"] = "application/json";
      opts.body = JSON.stringify(body);
    }
    return fetch(path, opts).then(function (res) {
      if (res.status === 401) {
        sessionStorage.removeItem(TOKEN_KEY);
        showGate("Invalid or expired admin token.");
        throw { handled: true };
      }
      return res.json().catch(function () { return {}; }).then(function (data) {
        return { status: res.status, ok: res.ok, data: data };
      });
    });
  }

  // ── views ──────────────────────────────────────────────────
  function showGate(errMsg) {
    hide(viewApp);
    closeModal();
    show(viewGate);
    gateToken.value = "";
    if (errMsg) {
      gateError.textContent = errMsg;
      show(gateError);
    } else {
      hide(gateError);
    }
    gateToken.focus();
  }

  function showApp() {
    hide(viewGate);
    show(viewApp);
    loadUsers();
  }

  function setListState(which) {
    [stLoading, stError, stEmpty, stTable].forEach(hide);
    if (which) show(which);
  }

  // ── users list ────────────────────────────────────────────
  function loadUsers() {
    setListState(stLoading);
    api("GET", API_USERS + "?page=" + state.page + "&per_page=" + state.perPage)
      .then(function (res) {
        if (!res.ok) {
          throw new Error(res.data && (res.data.message || res.data.error) || ("HTTP " + res.status));
        }
        state.users = res.data.users || [];
        state.total = res.data.total || state.users.length;
        renderUsers();
      })
      .catch(function (err) {
        if (err && err.handled) return;
        stErrorMsg.textContent = (err && err.message) ? err.message : "network error";
        setListState(stError);
      });
  }

  function renderUsers() {
    countNum.textContent = String(state.total);
    show(countChip);

    if (state.total === 0 || state.users.length === 0) {
      setListState(stEmpty);
      return;
    }

    tbody.textContent = "";
    state.users.forEach(function (u, i) {
      tbody.appendChild(buildRow(u, i));
    });

    var pages = Math.max(1, Math.ceil(state.total / state.perPage));
    pagerLabel.textContent = "page " + state.page + " / " + pages;
    pagerPrev.disabled = state.page <= 1;
    pagerNext.disabled = state.page >= pages;

    setListState(stTable);
  }

  function buildRow(u, index) {
    var tr = document.createElement("tr");
    tr.style.animationDelay = (index * 24) + "ms";

    // email
    var tdEmail = document.createElement("td");
    tdEmail.className = "cell-email";
    tdEmail.textContent = u.email || "—";
    tr.appendChild(tdEmail);

    // status badge
    var tdStatus = document.createElement("td");
    var badge = document.createElement("span");
    var deactivated = (u.status || "").toLowerCase() !== "active";
    badge.className = "badge " + (deactivated ? "badge-deactivated" : "badge-active");
    badge.textContent = deactivated ? (u.status || "deactivated") : "active";
    tdStatus.appendChild(badge);
    tr.appendChild(tdStatus);

    // created
    var tdCreated = document.createElement("td");
    tdCreated.className = "cell-created";
    tdCreated.textContent = humanize(u.created_at);
    if (u.created_at) tdCreated.title = u.created_at;
    tr.appendChild(tdCreated);

    // id (click to copy)
    var tdId = document.createElement("td");
    var chip = document.createElement("button");
    chip.type = "button";
    chip.className = "id-chip";
    var idStr = u.id || "";
    chip.textContent = idStr.length > 10 ? idStr.slice(0, 8) + "…" : idStr;
    chip.title = "Copy ID: " + idStr;
    chip.addEventListener("click", function () {
      copyText(idStr, "ID copied");
    });
    tdId.appendChild(chip);
    tr.appendChild(tdId);

    // actions: two-step deactivate
    var tdAct = document.createElement("td");
    tdAct.className = "cell-actions";
    if (!deactivated) {
      var del = document.createElement("button");
      del.type = "button";
      del.className = "btn btn-sm btn-danger-ghost";
      del.textContent = "Deactivate";
      del.addEventListener("click", function () {
        if (del.dataset.arm === "1") {
          del.disabled = true;
          del.textContent = "…";
          deactivateUser(u, badge, del);
        } else {
          del.dataset.arm = "1";
          del.classList.add("btn-confirm");
          del.textContent = "Confirm?";
          clearTimeout(state.confirmTimer);
          state.confirmTimer = setTimeout(function () {
            delete del.dataset.arm;
            del.classList.remove("btn-confirm");
            del.textContent = "Deactivate";
          }, 3200);
        }
      });
      tdAct.appendChild(del);
    }
    tr.appendChild(tdAct);

    return tr;
  }

  function deactivateUser(u, badge, btn) {
    api("DELETE", API_USERS + "/" + encodeURIComponent(u.id))
      .then(function (res) {
        if (!res.ok) {
          throw new Error(res.data && (res.data.message || res.data.error) || ("HTTP " + res.status));
        }
        badge.className = "badge badge-deactivated";
        badge.textContent = "deactivated";
        btn.remove();
        toast("User deactivated");
      })
      .catch(function (err) {
        if (err && err.handled) return;
        btn.disabled = false;
        btn.textContent = "Deactivate";
        delete btn.dataset.arm;
        btn.classList.remove("btn-confirm");
        toast("Deactivate failed: " + ((err && err.message) || "error"));
      });
  }

  // ── create modal ──────────────────────────────────────────
  var lastFocused = null;

  function openModal() {
    lastFocused = document.activeElement;
    show(backdrop);
    show(createForm);
    hide(createSuccess);
    hide(createError);
    createForm.reset();
    createSubmit.disabled = false;
    createSubmit.textContent = "Create identity";
    createEmail.focus();
  }

  function closeModal() {
    hide(backdrop);
    if (lastFocused && lastFocused.focus) lastFocused.focus();
  }

  function submitCreate(ev) {
    ev.preventDefault();
    hide(createError);
    var email = createEmail.value.trim();
    if (!email || createEmail.validity.typeMismatch) {
      createError.textContent = "Enter a valid email address.";
      show(createError);
      createEmail.focus();
      return;
    }
    createSubmit.disabled = true;
    createSubmit.textContent = "Creating…";

    api("POST", API_USERS, { email: email })
      .then(function (res) {
        if (res.status === 409) {
          throw new Error("A user with this email already exists.");
        }
        if (!res.ok) {
          throw new Error(res.data && (res.data.message || res.data.error) || ("HTTP " + res.status));
        }
        createdEmail.textContent = res.data.email || email;
        recoveryLink.textContent = res.data.recovery_link || "(no recovery link returned)";
        hide(createForm);
        show(createSuccess);
        $("btn-copy-link").focus();
        loadUsers();
      })
      .catch(function (err) {
        if (err && err.handled) return;
        createSubmit.disabled = false;
        createSubmit.textContent = "Create identity";
        createError.textContent = (err && err.message) || "Request failed.";
        show(createError);
      });
  }

  // ── wiring ────────────────────────────────────────────────
  gateForm.addEventListener("submit", function (ev) {
    ev.preventDefault();
    var t = gateToken.value.trim();
    if (!t) {
      gateError.textContent = "Enter your admin token.";
      show(gateError);
      return;
    }
    sessionStorage.setItem(TOKEN_KEY, t);
    hide(gateError);
    showApp();
  });

  $("btn-signout").addEventListener("click", function () {
    sessionStorage.removeItem(TOKEN_KEY);
    showGate();
  });

  $("btn-refresh").addEventListener("click", loadUsers);
  $("btn-retry").addEventListener("click", loadUsers);
  $("btn-add").addEventListener("click", openModal);
  $("btn-add-empty").addEventListener("click", openModal);

  pagerPrev.addEventListener("click", function () {
    if (state.page > 1) { state.page--; loadUsers(); }
  });
  pagerNext.addEventListener("click", function () {
    state.page++; loadUsers();
  });

  createForm.addEventListener("submit", submitCreate);
  $("create-cancel").addEventListener("click", closeModal);
  $("modal-close").addEventListener("click", closeModal);
  $("create-done").addEventListener("click", closeModal);

  $("btn-copy-link").addEventListener("click", function () {
    copyText(recoveryLink.textContent, "Recovery link copied");
  });

  backdrop.addEventListener("mousedown", function (ev) {
    if (ev.target === backdrop) closeModal();
  });

  document.addEventListener("keydown", function (ev) {
    if (ev.key === "Escape" && !backdrop.classList.contains("hidden")) {
      closeModal();
    }
  });

  // keep focus inside the modal while open
  document.addEventListener("focusin", function (ev) {
    if (backdrop.classList.contains("hidden")) return;
    if (!modal.contains(ev.target)) {
      var first = modal.querySelector("button, input");
      if (first) first.focus();
    }
  });

  // ── boot ──────────────────────────────────────────────────
  if (token()) {
    showApp();
  } else {
    showGate();
  }
})();
