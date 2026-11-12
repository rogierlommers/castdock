(() => {
  const content = document.querySelector("#network-events-content");
  const status = document.querySelector("#network-events-status");
  if (!content || !status) return;
  if (!window.EventSource) {
    status.textContent = "Unavailable";
    return;
  }

  const relativeTime = (value) => {
    const seconds = Math.max(0, Math.floor((Date.now() - new Date(value).getTime()) / 1000));
    if (seconds < 10) return "just now";
    if (seconds < 60) return `${seconds} seconds ago`;
    const minutes = Math.floor(seconds / 60);
    if (minutes === 1) return "one minute ago";
    if (minutes < 60) return `${minutes} minutes ago`;
    const hours = Math.floor(minutes / 60);
    if (hours === 1) return "one hour ago";
    if (hours < 24) return `${hours} hours ago`;
    const days = Math.floor(hours / 24);
    return days === 1 ? "one day ago" : `${days} days ago`;
  };

  const render = (events) => {
    content.replaceChildren();
    if (events.length === 0) {
      const empty = document.createElement("p");
      empty.className = "empty-state";
      empty.textContent = "No client network events recorded since startup.";
      content.append(empty);
      return;
    }

    const list = document.createElement("ol");
    list.className = "network-event-list";
    for (const event of events) {
      const item = document.createElement("li");
      const summary = document.createElement("div");
      summary.className = "network-event-summary";
      const description = document.createElement("strong");
      description.textContent = event.description;
      const client = document.createElement("span");
      client.className = "state-badge";
      client.textContent = event.client;
      summary.append(description, client);

      const details = document.createElement("p");
      const occurredAt = document.createElement("time");
      occurredAt.dateTime = event.occurredAt;
      occurredAt.dataset.occurredAt = event.occurredAt;
      occurredAt.title = new Date(event.occurredAt).toLocaleString();
      occurredAt.textContent = relativeTime(event.occurredAt);
      const request = document.createElement("code");
      request.textContent = `${event.method} ${event.path}`;
      details.append(occurredAt, " · ", request, ` · HTTP ${event.status}`);
      item.append(summary, details);
      list.append(item);
    }
    content.append(list);
  };

  const refreshRelativeTimes = () => {
    for (const time of content.querySelectorAll("time[data-occurred-at]")) {
      time.textContent = relativeTime(time.dataset.occurredAt);
    }
  };

  const source = new EventSource(content.dataset.streamUrl);
  source.addEventListener("network-events", (message) => {
    try {
      render(JSON.parse(message.data));
      status.textContent = "Live";
    } catch {
      status.textContent = "Invalid update";
    }
  });
  source.onerror = () => { status.textContent = "Reconnecting"; };
  window.setInterval(refreshRelativeTimes, 30000);
})();
