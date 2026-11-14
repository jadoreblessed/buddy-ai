#!/usr/bin/env python3
"""Measure local simple API reads after starting the mock and Go processes."""
import argparse
import json
import statistics
import time
import urllib.request


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument("--base", default="http://127.0.0.1:8080")
    parser.add_argument("--samples", type=int, default=30)
    args = parser.parse_args()
    if args.samples < 2:
        parser.error("--samples must be at least 2")
    results = {}
    for path in ("/api/plan", "/api/materials", "/api/requests/current"):
        samples = []
        for _ in range(args.samples + 1):
            start = time.perf_counter()
            with urllib.request.urlopen(args.base + path, timeout=5) as response:
                if response.status != 200:
                    raise RuntimeError(f"{path}: HTTP {response.status}")
                response.read()
            samples.append((time.perf_counter() - start) * 1000)
        samples = samples[1:]  # exclude first warm-up request
        ordered = sorted(samples)
        results[path] = {
            "median_ms": round(statistics.median(samples), 2),
            "p95_ms": round(ordered[int(.95 * (len(ordered) - 1))], 2),
            "max_ms": round(max(samples), 2),
            "samples": len(samples),
        }
    print(json.dumps(results, ensure_ascii=False, indent=2))


if __name__ == "__main__":
    main()
