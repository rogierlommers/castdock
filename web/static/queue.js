(() => {
  "use strict";

  const queue = document.getElementById("up-next-queue");
  if (!queue) return;

  const scrollKey = "castdock:scroll-to-up-next";
  if (window.sessionStorage.getItem(scrollKey) === "true") {
    window.sessionStorage.removeItem(scrollKey);
    window.requestAnimationFrame(() => queue.scrollIntoView({ block: "start" }));
  }

  let draggedItem = null;

  const items = () => [...queue.querySelectorAll(".queue-item")];

  function clearDropTargets() {
    for (const item of items()) item.classList.remove("drop-before", "drop-after");
  }

  for (const item of items()) {
    item.addEventListener("dragstart", (event) => {
      draggedItem = item;
      item.classList.add("dragging");
      event.dataTransfer.effectAllowed = "move";
      event.dataTransfer.setData("text/plain", item.dataset.episodeId);
    });

    item.addEventListener("dragover", (event) => {
      event.preventDefault();
      if (!draggedItem || draggedItem === item) return;
      clearDropTargets();
      const after = event.clientY > item.getBoundingClientRect().top + item.offsetHeight / 2;
      item.classList.add(after ? "drop-after" : "drop-before");
      event.dataTransfer.dropEffect = "move";
    });

    item.addEventListener("drop", async (event) => {
      event.preventDefault();
      if (!draggedItem || draggedItem === item) return;

      const sourcePosition = Number(draggedItem.dataset.queuePosition);
      let targetPosition = Number(item.dataset.queuePosition);
      const visiblePositions = items().map((queueItem) => Number(queueItem.dataset.queuePosition));
      const minimumPosition = Math.min(...visiblePositions);
      const maximumPosition = Math.max(...visiblePositions);
      const after = event.clientY > item.getBoundingClientRect().top + item.offsetHeight / 2;
      if (after) targetPosition += 1;
      if (sourcePosition < targetPosition) targetPosition -= 1;
      targetPosition = Math.max(minimumPosition, Math.min(maximumPosition, targetPosition));

      clearDropTargets();
      if (sourcePosition === targetPosition) return;
      try {
        const response = await fetch("/api/queue/reorder", {
          method: "POST",
          headers: { "Content-Type": "application/json" },
          body: JSON.stringify({
            episodeId: Number(draggedItem.dataset.episodeId),
            targetPosition,
          }),
        });
        if (!response.ok) throw new Error(`HTTP ${response.status}`);
        window.sessionStorage.setItem(scrollKey, "true");
        window.location.reload();
      } catch (error) {
        console.error("Unable to reorder Up Next", error);
        window.alert("CastDock could not save the new queue order.");
      }
    });

    item.addEventListener("dragend", () => {
      item.classList.remove("dragging");
      clearDropTargets();
      draggedItem = null;
    });
  }
})();
