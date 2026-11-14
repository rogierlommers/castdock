(() => {
  "use strict";

  const player = document.getElementById("player");
  const status = document.getElementById("sync-status");
  const playTimeLeft = document.getElementById("play-time-left");
  const playbackRateSelect = document.getElementById("player-playback-rate");
  const markPlayedForm = document.getElementById("mark-playing-played");
  const showNotesButton = document.getElementById("show-player-notes");
  const showNotesDialog = document.getElementById("player-notes-dialog");
  const closeNotesButton = document.getElementById("close-player-notes");
  const rewindButton = document.getElementById("rewind-15");
  const forwardButton = document.getElementById("forward-15");
  if (!player) return;

  const episodeID = Number(player.dataset.episodeId);
  const feedID = Number(player.dataset.feedId);
  const savedPosition = Number(player.dataset.position || 0);
  const configuredPlaybackRate = Number(player.dataset.playbackRate || 1);
  const stayOnHome = player.dataset.stayOnHome === "true";
  const shouldAutoplay = player.dataset.autoplay === "true";
  let restored = false;
  let autoplayAttempted = false;
  let saveChain = Promise.resolve({});
  let confirmedPlaybackRate = configuredPlaybackRate;
  let playbackRateSaveTimer;
  let playbackRateSaveChain = Promise.resolve();
  let suppressPlaybackSaves = false;

  function formatPlayTime(seconds) {
    const totalMinutes = Math.ceil(seconds / 60);
    if (totalMinutes < 60) return `${totalMinutes} ${totalMinutes === 1 ? "minute" : "minutes"}`;

    const hours = Math.floor(totalMinutes / 60);
    const minutes = totalMinutes % 60;
    const hourLabel = `${hours} ${hours === 1 ? "hour" : "hours"}`;
    if (minutes === 0) return hourLabel;
    return `${hourLabel} ${minutes} ${minutes === 1 ? "minute" : "minutes"}`;
  }

  function updatePlayTimeLeft() {
    const duration = player.duration;
    const currentTime = player.currentTime;
    const playbackRate = player.playbackRate;
    if (!playTimeLeft || !Number.isFinite(duration) || !Number.isFinite(currentTime) || !Number.isFinite(playbackRate) || playbackRate <= 0) {
      if (playTimeLeft) playTimeLeft.hidden = true;
      return;
    }

    const secondsLeft = Math.max(0, duration - currentTime) / playbackRate;
    playTimeLeft.textContent = `Play time left: ${formatPlayTime(secondsLeft)}`;
    playTimeLeft.hidden = false;
  }

  async function persistPlaybackRate(rate) {
    try {
      const response = await fetch(`/api/feeds/${feedID}/playback-options`, {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({ playbackSpeed: rate }),
      });
      if (!response.ok) throw new Error(`HTTP ${response.status}`);
      confirmedPlaybackRate = rate;
    } catch (error) {
      console.error("Unable to save playback speed", error);
      if (player.playbackRate === rate) {
        player.playbackRate = confirmedPlaybackRate;
        if (playbackRateSelect) playbackRateSelect.value = String(confirmedPlaybackRate);
      }
      status.hidden = false;
      status.textContent = "Playback speed could not be saved.";
    }
  }

  function schedulePlaybackRateSave() {
    const rate = player.playbackRate;
    if (!restored || !Number.isFinite(rate) || rate < 0.5 || rate > 3 || rate === confirmedPlaybackRate) return;
    if (playbackRateSelect) playbackRateSelect.value = String(rate);
    window.clearTimeout(playbackRateSaveTimer);
    playbackRateSaveTimer = window.setTimeout(() => {
      playbackRateSaveChain = playbackRateSaveChain.then(() => persistPlaybackRate(rate));
    }, 300);
  }

  const payload = () => ({
    position: Number.isFinite(player.currentTime) ? player.currentTime : 0,
    duration: Number.isFinite(player.duration) ? player.duration : 0,
    completed: player.ended,
    playing: !player.paused && !player.ended,
    clientUpdatedAt: new Date().toISOString(),
  });

  async function send(state) {
    status.hidden = false;
    try {
      const response = await fetch(`/api/episodes/${episodeID}/playback`, {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify(state),
        keepalive: true,
      });
      if (!response.ok) throw new Error(`HTTP ${response.status}`);
      console.info("Playback state succesfully sent:", state);
      return await response.json();
    } catch (error) {
      console.error("Unable to save playback position", error);
      status.hidden = false;
      status.textContent = "Playback position could not be saved; CastDock will retry.";
      return {};
    }
  }

  function save() {
    if (suppressPlaybackSaves) return Promise.resolve({});
    const state = payload();
    saveChain = saveChain.then(() => send(state));
    return saveChain;
  }

  function beacon() {
    if (suppressPlaybackSaves) return;
    const body = new Blob([JSON.stringify(payload())], { type: "application/json" });
    navigator.sendBeacon(`/api/episodes/${episodeID}/playback`, body);
  }

  function handleLoadedMetadata() {
    if (Number.isFinite(configuredPlaybackRate) && configuredPlaybackRate >= 0.5 && configuredPlaybackRate <= 3) {
      player.defaultPlaybackRate = configuredPlaybackRate;
      player.playbackRate = configuredPlaybackRate;
    }
    if (!restored && savedPosition > 0 && savedPosition < player.duration - 1) {
      player.currentTime = savedPosition;
    }
    restored = true;
    rewindButton.disabled = false;
    forwardButton.disabled = false;
    updatePlayTimeLeft();
    if (shouldAutoplay && !autoplayAttempted) {
      autoplayAttempted = true;
      player.play().catch(() => {});
    }
  }

  player.addEventListener("loadedmetadata", handleLoadedMetadata);
  if (player.readyState >= 1) handleLoadedMetadata();
  for (const event of ["timeupdate", "durationchange"]) {
    player.addEventListener(event, updatePlayTimeLeft);
  }
  player.addEventListener("ratechange", () => {
    updatePlayTimeLeft();
    schedulePlaybackRateSave();
  });
  playbackRateSelect?.addEventListener("change", () => {
    const rate = Number(playbackRateSelect.value);
    if (Number.isFinite(rate) && rate >= 0.5 && rate <= 3) player.playbackRate = rate;
  });
  markPlayedForm?.addEventListener("submit", () => {
    suppressPlaybackSaves = true;
  });
  showNotesButton?.addEventListener("click", () => showNotesDialog?.showModal());
  closeNotesButton?.addEventListener("click", () => showNotesDialog?.close());

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
    if (result.nextEpisodeUrl) {
      if (stayOnHome) {
        const nextEpisodeID = result.nextEpisodeUrl.split("/").filter(Boolean).pop();
        window.location.assign(`/?play=${encodeURIComponent(nextEpisodeID)}#now-playing`);
      } else {
        window.location.assign(result.nextEpisodeUrl);
      }
    }
  });
  window.setInterval(() => {
    if (!player.paused && !player.ended) void save();
  }, 5000);
  window.addEventListener("pagehide", beacon);
})();
