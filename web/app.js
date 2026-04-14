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

function summarizeResults(results) {
  const byURL = {};
  let successCount = 0;
  let errorCount = 0;

  results.forEach((result) => {
    const key = result.url || "(unknown url)";
    if (!byURL[key]) {
      byURL[key] = [];
    }
    byURL[key].push(result);

    if (result.error) {
      errorCount += 1;
    } else {
      successCount += 1;
    }
  });

  return {
    byURL,
    successCount,
    errorCount,
    totalCount: results.length
  };
}

function parseSSEEvent(block) {
  const lines = block.split("\n");
  let eventType = "message";
  let data = "";

  lines.forEach((line) => {
    if (line.startsWith("event:")) {
      eventType = line.slice(6).trim();
    }
    if (line.startsWith("data:")) {
      data += line.slice(5).trim();
    }
  });

  if (!data) {
    return null;
  }

  let parsedData = null;
  try {
    parsedData = JSON.parse(data);
  } catch (_) {
    return null;
  }

  return { type: eventType, data: parsedData };
}

function renderResults(results) {
  resultsEl.innerHTML = "";
  const summary = summarizeResults(results);

  const summaryBar = document.createElement("section");
  summaryBar.className = "result-summary";
  summaryBar.innerHTML = "<strong>Run Summary</strong>"
    + "<span class=\"badge success\">Success: " + summary.successCount + "</span>"
    + "<span class=\"badge error\">Failed: " + summary.errorCount + "</span>"
    + "<span class=\"badge neutral\">Total: " + summary.totalCount + "</span>";
  resultsEl.appendChild(summaryBar);

  Object.keys(summary.byURL).forEach((url) => {
    const urlResults = summary.byURL[url];
    const urlErrors = urlResults.filter((item) => item.error).length;

    const group = document.createElement("section");
    group.className = "url-group";

    const groupHead = document.createElement("div");
    groupHead.className = "url-head";
    groupHead.innerHTML = "<strong>" + url + "</strong>"
      + "<span class=\"badge " + (urlErrors > 0 ? "error" : "success") + "\">"
      + (urlErrors > 0 ? "Partial/Failed" : "Success")
      + "</span>";
    group.appendChild(groupHead);

    const groupBody = document.createElement("div");
    groupBody.className = "url-body";

    urlResults.forEach((result) => {
      const card = document.createElement("article");
      card.className = "result-card " + (result.error ? "error" : "success");

      const head = document.createElement("div");
      head.className = "head";

      const meta = document.createElement("span");
      meta.textContent = "Account: " + (result.account || "(no account)");
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
      groupBody.appendChild(card);
    });

    group.appendChild(groupBody);
    resultsEl.appendChild(group);
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
  setBusy(true, "Processing " + urls.length + " URL(s)... This may take a bit due to live scraping.");
  resultsEl.innerHTML = "";

  const streamedResults = [];

  try {
    const resp = await fetch("/generate/stream", {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ urls, accounts })
    });

    if (!resp.ok) {
	    const payload = await resp.json();
      throw new Error(payload.error || "generation failed");
    }

    if (!resp.body || !resp.body.getReader) {
      throw new Error("streaming is not supported by this browser");
    }

    const reader = resp.body.getReader();
    const decoder = new TextDecoder();
    let buffer = "";

    while (true) {
      const read = await reader.read();
      if (read.done) {
        break;
      }

      buffer += decoder.decode(read.value, { stream: true });

      let splitIndex = buffer.indexOf("\n\n");
      while (splitIndex !== -1) {
        const rawEvent = buffer.slice(0, splitIndex);
        buffer = buffer.slice(splitIndex + 2);

        const event = parseSSEEvent(rawEvent);
        if (event) {
          if (event.type === "progress") {
            statusEl.textContent = "Processing " + event.data.current + "/" + event.data.total + ": " + event.data.url;
          }

          if (event.type === "result") {
            streamedResults.push(event.data.result);
            renderResults(streamedResults);
          }

          if (event.type === "error") {
            statusEl.textContent = "Error: " + event.data.error;
          }

          if (event.type === "done") {
            statusEl.textContent = "Done. Success: " + event.data.success
              + ", Failed: " + event.data.failed
              + ", Total: " + event.data.totalResults + ".";
          }
        }

        splitIndex = buffer.indexOf("\n\n");
      }
    }
  } catch (err) {
    statusEl.textContent = "Error: " + err.message;
  } finally {
    setBusy(false, statusEl.textContent);
  }
});

loadAccounts().catch((err) => {
  statusEl.textContent = "Error: " + err.message;
});
