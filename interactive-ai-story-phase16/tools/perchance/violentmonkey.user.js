// ==UserScript==
// @name         Interactive Story Perchance Transport
// @namespace    local.interactive-story
// @version      1.2.0
// @description  Localhost <-> dedicated Perchance worker via a scoped DOM mailbox.
// @match        https://perchance.org/interactive-ai-story-worker*
// @match        https://*.perchance.org/interactive-ai-story-worker*
// @grant        GM_xmlhttpRequest
// @connect      127.0.0.1
// @run-at       document-idle
// ==/UserScript==
(() => {
  'use strict';

  const API = ['http:', '', '127.0.0.1:8787'].join('/');
  const WORKER_ID = 'browser-local-1';
  const POLL_MS = 1500;
  const HEARTBEAT_MS = 10000;
  const ACTIVE_JOB_STALE_MS = 6 * 60 * 1000;

  if (location.pathname !== '/interactive-ai-story-worker') return;

  let activeJob = null;
  let activeJobClaimedAt = 0;
  let postingResult = false;
  let resultUploadAttempts = 0;
  let observer = null;
  let observedResultBox = null;

  const statusBox = document.createElement('div');
  Object.assign(statusBox.style, {
    position: 'fixed',
    right: '12px',
    bottom: '12px',
    zIndex: '2147483647',
    padding: '8px 10px',
    maxWidth: '420px',
    borderRadius: '8px',
    border: '1px solid rgba(120,170,255,.45)',
    background: 'rgba(10,10,12,.94)',
    color: '#9ec5ff',
    font: '12px monospace',
    whiteSpace: 'pre-wrap',
    pointerEvents: 'none',
  });
  document.documentElement.appendChild(statusBox);

  function setStatus(text) {
    statusBox.textContent = `LOCAL TRANSPORT\n${text}`;
    console.log('[interactive-story-transport]', text);
  }

  function request(method, path, body) {
    return new Promise((resolve, reject) => {
      GM_xmlhttpRequest({
        method,
        url: API + path,
        headers: {
          'Content-Type': 'application/json',
          'X-Image-Worker-ID': WORKER_ID,
        },
        data: body === undefined ? undefined : JSON.stringify(body),
        timeout: 30000,
        onload: r => resolve(r),
        onerror: reject,
        ontimeout: () => reject(new Error('localhost request timeout')),
      });
    });
  }

  function localBoxes() {
    return {
      job: document.getElementById('go-perchance-job-mailbox'),
      result: document.getElementById('go-perchance-result-mailbox'),
    };
  }

  function runtimeReady() {
    const {job, result} = localBoxes();
    return Boolean(job && result);
  }

  function deliverJob(jobPayload) {
    const {job, result} = localBoxes();
    if (!job || !result) throw new Error('dedicated Perchance runtime mailbox is unavailable');
    if (result) result.textContent = '';
    job.textContent = JSON.stringify(jobPayload);
    job.dataset.version = String(Date.now());
    setStatus(`claimed ${jobPayload.id}\ndelivered by scoped DOM mailbox`);
  }

  async function heartbeat() {
    if (!runtimeReady()) return;
    try {
      const r = await request('POST', '/internal/image-worker/heartbeat', {
        workerId: WORKER_ID,
        activeGenerationId: activeJob?.id ?? null,
      });
      if (r.status < 200 || r.status >= 300) {
        setStatus(`heartbeat HTTP ${r.status}`);
      }
    } catch (error) {
      setStatus(`heartbeat error: ${error?.message || error}`);
    }
  }

  async function poll() {
    if (!runtimeReady()) {
      setStatus('waiting for dedicated runtime mailbox');
      return;
    }
    if (activeJob) {
      if (Date.now() - activeJobClaimedAt < ACTIVE_JOB_STALE_MS) return;
      setStatus(`local job watchdog released ${activeJob.id}`);
      activeJob = null;
      activeJobClaimedAt = 0;
      resultUploadAttempts = 0;
    }

    try {
      const r = await request('GET', `/internal/image-worker/jobs/next?worker_id=${encodeURIComponent(WORKER_ID)}`);
      if (r.status === 204) {
        setStatus('connected\nqueue empty');
        return;
      }
      if (r.status !== 200) {
        setStatus(`claim HTTP ${r.status}\n${String(r.responseText || '').slice(0, 240)}`);
        return;
      }

      const next = JSON.parse(r.responseText);
      activeJob = next;
      activeJobClaimedAt = Date.now();
      resultUploadAttempts = 0;
      deliverJob(next);
      void heartbeat();
    } catch (error) {
      setStatus(`poll error: ${error?.message || error}`);
    }
  }

  function readDOMResult() {
    const {result} = localBoxes();
    if (!result || !result.textContent.trim()) return null;
    try { return JSON.parse(result.textContent); } catch (_) { return null; }
  }

  async function forwardResult(payloadOverride) {
    if (postingResult || !activeJob) return;
    const payload = payloadOverride ?? readDOMResult();
    if (!payload || String(payload.id) !== String(activeJob.id)) return;

    postingResult = true;
    try {
      const path = payload.error
        ? `/internal/image-worker/jobs/${activeJob.id}/failed`
        : `/internal/image-worker/jobs/${activeJob.id}/result`;
      const body = payload.error ? {error: String(payload.error)} : {images: payload.images};
      const r = await request('POST', path, body);

      if (r.status >= 200 && r.status < 300) {
        const {job, result} = localBoxes();
        if (job) job.textContent = '';
        if (result) result.textContent = '';
        setStatus(`completed ${activeJob.id}\nHTTP ${r.status}`);
        activeJob = null;
        activeJobClaimedAt = 0;
        resultUploadAttempts = 0;
        void heartbeat();
        void poll();
        return;
      }

      await retryOrReleaseResult(payload, `result upload rejected with HTTP ${r.status}`);
    } catch (error) {
      await retryOrReleaseResult(payload, `result upload error: ${error?.message || error}`);
    } finally {
      postingResult = false;
    }
  }

  async function retryOrReleaseResult(payload, reason) {
    resultUploadAttempts++;
    setStatus(`${reason}\nattempt ${resultUploadAttempts}/3`);
    if (resultUploadAttempts < 3) {
      setTimeout(() => { void forwardResult(payload); }, 3000);
      return;
    }

    try {
      await request('POST', `/internal/image-worker/jobs/${activeJob.id}/failed`, {error: reason});
    } catch (_) {
      // The backend lease will make the job claimable again after recovery.
    }
    activeJob = null;
    activeJobClaimedAt = 0;
    resultUploadAttempts = 0;
    void heartbeat();
    setTimeout(() => { void poll(); }, 3000);
  }

  function attachDOMObserver() {
    const {result} = localBoxes();
    if (!result || result === observedResultBox) return;
    if (observer) observer.disconnect();
    observer = new MutationObserver(() => { void forwardResult(); });
    observer.observe(result, {childList:true, characterData:true, subtree:true, attributes:true});
    observedResultBox = result;
  }

  setInterval(attachDOMObserver, 1000);
  setInterval(() => { void poll(); }, POLL_MS);
  setInterval(() => { void heartbeat(); }, HEARTBEAT_MS);

  attachDOMObserver();
  if (runtimeReady()) {
    setStatus('starting...');
    void heartbeat();
    void poll();
  } else {
    setStatus('waiting for dedicated runtime mailbox');
  }
})();
