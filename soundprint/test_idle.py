"""Tests for idle unloading (run with: python3 -m unittest test_idle). Needs no GPU, torch
or model downloads; when torch is installed, a real CPU module is loaded and freed too."""

import gc
import threading
import time
import unittest
import weakref

from idle import IdleModel, start_reaper


class Model:
    """Stands in for a loaded network."""


class Clock:
    def __init__(self):
        self.t = 1000.0

    def __call__(self):
        return self.t


class IdleModelTest(unittest.TestCase):
    def test_loads_on_demand_unloads_when_idle_and_reloads(self):
        loads, freed = [], []
        clock = Clock()

        def load():
            loads.append(1)
            return Model()

        m = IdleModel("clap", load, idle_seconds=300, clock=clock, on_unload=lambda: freed.append(1))
        self.assertFalse(m.loaded)
        self.assertEqual(loads, [], "nothing loads before the first request")
        first = m.get()
        ref = weakref.ref(first)
        del first
        self.assertTrue(m.loaded)
        clock.t += 299
        self.assertFalse(m.unload_if_idle(), "still within the idle time")
        m.get()  # use resets the timer
        clock.t += 299
        self.assertFalse(m.unload_if_idle())
        clock.t += 2
        self.assertTrue(m.unload_if_idle())
        self.assertFalse(m.loaded)
        self.assertEqual(freed, [1], "GPU memory is released on unload")
        gc.collect()
        self.assertIsNone(ref(), "the model object is gone after unloading")
        self.assertFalse(m.unload_if_idle(), "nothing to unload twice")
        m.get()
        self.assertEqual(len(loads), 2, "the next request loads it again")

    def test_concurrent_requests_load_once(self):
        loads = []

        def load():
            time.sleep(0.05)
            loads.append(1)
            return Model()

        m = IdleModel("bge", load, idle_seconds=60, on_unload=lambda: None)
        got = []
        threads = [threading.Thread(target=lambda: got.append(m.get())) for _ in range(8)]
        for t in threads:
            t.start()
        for t in threads:
            t.join()
        self.assertEqual(len(loads), 1)
        self.assertTrue(all(g is got[0] for g in got))

    def test_zero_disables_unloading(self):
        clock = Clock()
        m = IdleModel("clap", Model, idle_seconds=0, clock=clock, on_unload=lambda: None)
        m.get()
        clock.t += 10_000
        self.assertFalse(m.unload_if_idle())
        self.assertTrue(m.loaded)

    def test_reaper_unloads_in_background(self):
        m = IdleModel("clap", Model, idle_seconds=0.05, on_unload=lambda: None)
        lock = threading.Lock()
        m.get()
        start_reaper([m], lock, every=0.02)
        deadline = time.time() + 2
        while m.loaded and time.time() < deadline:
            time.sleep(0.01)
        self.assertFalse(m.loaded)

    def test_reaper_never_unloads_mid_use(self):
        m = IdleModel("clap", Model, idle_seconds=0.01, on_unload=lambda: None)
        lock = threading.Lock()
        start_reaper([m], lock, every=0.01)
        with lock:  # a request is using the model
            m.get()
            time.sleep(0.1)
            self.assertTrue(m.loaded)

    def test_real_torch_module_on_cpu(self):
        try:
            import torch
        except ImportError:
            self.skipTest("torch not installed")
        m = IdleModel("linear", lambda: torch.nn.Linear(256, 256), idle_seconds=0.01)
        ref = weakref.ref(m.get())
        time.sleep(0.02)
        self.assertTrue(m.unload_if_idle())
        gc.collect()
        self.assertIsNone(ref())
        self.assertEqual(m.get().in_features, 256)


if __name__ == "__main__":
    unittest.main()
