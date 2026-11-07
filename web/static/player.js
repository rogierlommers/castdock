(() => {
  "use strict";

  const player = document.getElementById("player");
  const status = document.getElementById("sync-status");
  if (!player) return;

  const episodeID = Number(player.dataset.episodeId);
  const savedPosition = Number(player.dataset.position || 0);
  let restored = false;
  let saving = false;
  let queued = false;

  const payload = () => ({
    position: Number.isFinite(player.currentTime) ? player.currentTime : 0,
    duration: Number.isFinite(player.duration) ? player.duration : 0,
    completed: player.ended,
    playing: !player.paused && !player.ended,
    clientUpdatedAt: new Date().toISOString(),
  });

  async function save() {
    if (saving) {
      queued = true;
      return;
    }
    saving = true;
    status.textContent = "Saving playback position…";
    try {
      const response = await fetch(`/api/episodes/${episodeID}/playback`, {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify(payload()),
        keepalive: true,
      });
      if (!response.ok) throw new Error(`HTTP ${response.status}`);
      status.textContent = "Playback position saved.";
    } catch (error) {
      console.error("Unable to save playback position", error);
      status.textContent = "Playback position could not be saved; CastDock will retry.";
    } finally {
      saving = false;
      if (queued) {
        queued = false;
        void save();
      }
    }
  }

  function beacon() {
    const body = new Blob([JSON.stringify(payload())], { type: "application/json" });
    navigator.sendBeacon(`/api/episodes/${episodeID}/playback`, body);
  }

  player.addEventListener("loadedmetadata", () => {
    if (!restored && savedPosition > 0 && savedPosition < player.duration - 1) {
      player.currentTime = savedPosition;
    }
    restored = true;
  });
  for (const event of ["play", "pause", "seeked", "ended"]) {
    player.addEventListener(event, () => void save());
  }
  window.setInterval(() => {
    if (!player.paused && !player.ended) void save();
  }, 5000);
  window.addEventListener("pagehide", beacon);
})();
