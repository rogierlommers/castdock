(() => {
  "use strict";

  const player = document.getElementById("player");
  const status = document.getElementById("sync-status");
  const rewindButton = document.getElementById("rewind-15");
  const forwardButton = document.getElementById("forward-15");
  if (!player) return;

  const episodeID = Number(player.dataset.episodeId);
  const savedPosition = Number(player.dataset.position || 0);
  let restored = false;
  let saveChain = Promise.resolve({});

  const payload = () => ({
    position: Number.isFinite(player.currentTime) ? player.currentTime : 0,
    duration: Number.isFinite(player.duration) ? player.duration : 0,
    completed: player.ended,
    playing: !player.paused && !player.ended,
    clientUpdatedAt: new Date().toISOString(),
  });

  async function send(state) {
    status.textContent = "Saving playback position…";
    try {
      const response = await fetch(`/api/episodes/${episodeID}/playback`, {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify(state),
        keepalive: true,
      });
      if (!response.ok) throw new Error(`HTTP ${response.status}`);
      status.textContent = "Playback position saved.";
      return await response.json();
    } catch (error) {
      console.error("Unable to save playback position", error);
      status.textContent = "Playback position could not be saved; CastDock will retry.";
      return {};
    }
  }

  function save() {
    const state = payload();
    saveChain = saveChain.then(() => send(state));
    return saveChain;
  }

  function beacon() {
    const body = new Blob([JSON.stringify(payload())], { type: "application/json" });
    navigator.sendBeacon(`/api/episodes/${episodeID}/playback`, body);
  }

  function handleLoadedMetadata() {
    if (!restored && savedPosition > 0 && savedPosition < player.duration - 1) {
      player.currentTime = savedPosition;
    }
    restored = true;
    rewindButton.disabled = false;
    forwardButton.disabled = false;
  }

  player.addEventListener("loadedmetadata", handleLoadedMetadata);
  if (player.readyState >= 1) handleLoadedMetadata();

  function seekBy(seconds) {
    const current = Number.isFinite(player.currentTime) ? player.currentTime : 0;
    const upperBound = Number.isFinite(player.duration) ? player.duration : current + Math.max(seconds, 0);
    player.currentTime = Math.max(0, Math.min(upperBound, current + seconds));
  }

  rewindButton.addEventListener("click", () => seekBy(-15));
  forwardButton.addEventListener("click", () => seekBy(15));
  for (const event of ["play", "pause", "seeked"]) {
    player.addEventListener(event, () => void save());
  }
  player.addEventListener("ended", async () => {
    const result = await save();
    if (result.nextEpisodeUrl) window.location.assign(result.nextEpisodeUrl);
  });
  window.setInterval(() => {
    if (!player.paused && !player.ended) void save();
  }, 5000);
  window.addEventListener("pagehide", beacon);
})();
