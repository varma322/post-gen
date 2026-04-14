const accountsEl = document.getElementById("accounts");
const generateBtn = document.getElementById("generate");
const selectAllBtn = document.getElementById("selectAll");
const clearBtn = document.getElementById("clear");
const statusEl = document.getElementById("status");
const resultsEl = document.getElementById("results");
const urlsEl = document.getElementById("urls");

async function loadAccounts() {
  statusEl.textContent = "Loading accounts...";
  const resp = await fetch("/accounts");
  if (!resp.ok) {
    throw new Error("failed to load accounts");
  }
  const payload = await resp.json();
  renderAccounts(payload.accounts || []);
  statusEl.textContent = "Accounts loaded.";
}

function renderAccounts(accounts) {
  accountsEl.innerHTML = "";
  accounts.forEach((account, idx) => {
    const id = "acc-" + idx;
    const wrapper = document.createElement("label");
    wrapper.className = "account-item";
    wrapper.htmlFor = id;

    const input = document.createElement("input");
    input.type = "checkbox";
    input.id = id;
    input.value = account.name;

    const text = document.createElement("span");
    text.textContent = account.name;

    wrapper.appendChild(input);
    wrapper.appendChild(text);
    accountsEl.appendChild(wrapper);
  });
}

function getSelectedAccounts() {
  return Array.from(accountsEl.querySelectorAll("input[type=checkbox]:checked")).map((el) => el.value);
}

function parseURLs() {
  return urlsEl.value
    .split(/\r?\n/)
    .map((v) => v.trim())
    .filter(Boolean);
}

function setBusy(isBusy, text) {
  generateBtn.disabled = isBusy;
  selectAllBtn.disabled = isBusy;
  clearBtn.disabled = isBusy;
  statusEl.textContent = text || "";
}

function renderResults(results) {
  resultsEl.innerHTML = "";
  results.forEach((result) => {
    const card = document.createElement("article");
    card.className = "result-card " + (result.error ? "error" : "success");

    const head = document.createElement("div");
    head.className = "head";

    const meta = document.createElement("span");
    meta.textContent = (result.url || "(unknown url)") + " | " + (result.account || "(no account)");
    head.appendChild(meta);

    if (!result.error && result.output) {
      const copyBtn = document.createElement("button");
      copyBtn.className = "secondary";
      copyBtn.type = "button";
      copyBtn.textContent = "Copy";
      copyBtn.onclick = async () => {
        try {
          await navigator.clipboard.writeText(result.output);
          copyBtn.textContent = "Copied";
          setTimeout(() => { copyBtn.textContent = "Copy"; }, 1200);
        } catch (_) {
          copyBtn.textContent = "Copy failed";
        }
      };
      head.appendChild(copyBtn);
    }

    const body = document.createElement("div");
    body.className = "body";

    if (result.error) {
      const err = document.createElement("p");
      err.className = "err";
      err.textContent = result.error;
      body.appendChild(err);
    } else {
      const pre = document.createElement("pre");
      pre.textContent = result.output || "";
      body.appendChild(pre);
    }

    card.appendChild(head);
    card.appendChild(body);
    resultsEl.appendChild(card);
  });
}

selectAllBtn.addEventListener("click", () => {
  accountsEl.querySelectorAll("input[type=checkbox]").forEach((el) => { el.checked = true; });
});

clearBtn.addEventListener("click", () => {
  urlsEl.value = "";
  resultsEl.innerHTML = "";
  statusEl.textContent = "Cleared.";
});

generateBtn.addEventListener("click", async () => {
  const urls = parseURLs();
  if (urls.length === 0) {
    statusEl.textContent = "Enter at least one URL.";
    return;
  }

  const accounts = getSelectedAccounts();
  setBusy(true, "Processing " + urls.length + " URL(s)...");

  try {
    const resp = await fetch("/generate", {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ urls, accounts })
    });

    const payload = await resp.json();
    if (!resp.ok) {
      throw new Error(payload.error || "generation failed");
    }

    renderResults(payload.results || []);
    const resultCount = payload && payload.results ? payload.results.length : 0;
    statusEl.textContent = "Done. " + resultCount + " result(s).";
  } catch (err) {
    statusEl.textContent = "Error: " + err.message;
  } finally {
    setBusy(false, statusEl.textContent);
  }
});

loadAccounts().catch((err) => {
  statusEl.textContent = "Error: " + err.message;
});
