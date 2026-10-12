"""Models that load on first use and unload after a while unused, so the GPU is free when
nothing is being analysed (the sidecar shares the GPU with transcoding and other apps).

Callers hold the GPU lock around get() and the use of the model, and the reaper takes the
same lock before unloading, so a model is never dropped mid-use.
"""

import gc
import logging
import threading
import time

log = logging.getLogger("soundprint")


def free_gpu_memory():
    """Returns cached CUDA memory to the driver (no-op without CUDA or torch)."""
    gc.collect()
    try:
        import torch

        if torch.cuda.is_available():
            torch.cuda.empty_cache()
    except ImportError:
        pass


class IdleModel:
    """Holds whatever load() returns until it has been idle for idle_seconds."""

    def __init__(self, name, load, idle_seconds, clock=time.monotonic, on_unload=free_gpu_memory):
        self.name = name
        self._load = load
        self.idle_seconds = idle_seconds
        self._clock = clock
        self._on_unload = on_unload
        self._value = None
        self._last_used = 0.0
        self._mu = threading.Lock()  # guards _value against concurrent loads

    @property
    def loaded(self):
        return self._value is not None

    def get(self):
        """Returns the model, loading it if needed. Call while holding the GPU lock."""
        with self._mu:
            if self._value is None:
                log.info("loading %s", self.name)
                self._value = self._load()
            self._last_used = self._clock()
            return self._value

    def unload_if_idle(self):
        """Drops the model when unused for idle_seconds; returns whether it did. Call while
        holding the GPU lock."""
        with self._mu:
            if self._value is None or self.idle_seconds <= 0:
                return False
            if self._clock() - self._last_used < self.idle_seconds:
                return False
            log.info("unloading %s after %.0f minutes idle", self.name, self.idle_seconds / 60)
            self._value = None
        self._on_unload()
        return True


def start_reaper(models, gpu_lock, every=30.0):
    """Checks the models every few seconds in a daemon thread and unloads idle ones."""

    def loop():
        while True:
            time.sleep(every)
            # Don't wait behind a long analysis batch: try again next round.
            if gpu_lock.acquire(timeout=1):
                try:
                    for m in models:
                        m.unload_if_idle()
                except Exception:  # noqa: BLE001 — the reaper must keep running
                    log.exception("idle unload failed")
                finally:
                    gpu_lock.release()

    t = threading.Thread(target=loop, name="idle-unload", daemon=True)
    t.start()
    return t
