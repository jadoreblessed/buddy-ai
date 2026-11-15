#!/usr/bin/env python3
"""Reproduce AI-buddy checks for three positions and request states."""
import argparse
import json
import os
import signal
import socket
import statistics
import subprocess
import sys
import tempfile
import time
import urllib.error
import urllib.request
from datetime import datetime, timezone
from pathlib import Path

ROOT = Path(__file__).resolve().parent
POSITIONS = ("backend", "frontend", "analyst")


def free_port():
    with socket.socket() as sock:
        sock.bind(("127.0.0.1", 0))
        return sock.getsockname()[1]


def call(base, path, method="GET", body=None, headers=None, timeout=30):
    data = None if body is None else json.dumps(body).encode()
    request = urllib.request.Request(base + path, data=data, method=method)
    request.add_header("Content-Type", "application/json")
    for key, value in (headers or {}).items():
        request.add_header(key, value)
    try:
        with urllib.request.urlopen(request, timeout=timeout) as response:
            payload = response.read()
            return response.status, json.loads(payload) if payload else None
    except urllib.error.HTTPError as error:
        payload = error.read()
        try:
            parsed = json.loads(payload) if payload else None
        except json.JSONDecodeError:
            parsed = {"error": payload.decode(errors="replace")}
        return error.code, parsed


def wait_health(base, seconds=40):
    deadline = time.monotonic() + seconds
    while time.monotonic() < deadline:
        try:
            status, _ = call(base, "/health", timeout=1)
            if status == 200:
                return
        except (OSError, urllib.error.URLError):
            pass
        time.sleep(0.25)
    raise RuntimeError(f"service did not become healthy: {base}")


def start_services():
    data_dir = tempfile.TemporaryDirectory(prefix="buddy-eval-")
    Path(data_dir.name).mkdir(parents=True, exist_ok=True)
    mock_port, api_port = free_port(), free_port()
    env = os.environ.copy()
    env.update({
        "MOCK_SERVICE_ADDR": f"127.0.0.1:{mock_port}",
        "MOCK_DATA_DIR": data_dir.name,
        "API_ADDR": f"127.0.0.1:{api_port}",
        "TICKET_SERVICE_URL": f"http://127.0.0.1:{mock_port}",
        "ONBOARDING_SERVICE_URL": f"http://127.0.0.1:{mock_port}",
    })
    log_path = Path(data_dir.name) / "services.log"
    log = log_path.open("w")
    processes = [
        subprocess.Popen(["go", "run", "./mock-service"], cwd=ROOT, env=env,
                         stdout=log, stderr=subprocess.STDOUT, start_new_session=True),
        subprocess.Popen(["go", "run", "./backend"], cwd=ROOT, env=env,
                         stdout=log, stderr=subprocess.STDOUT, start_new_session=True),
    ]
    mock_base = f"http://127.0.0.1:{mock_port}"
    api_base = f"http://127.0.0.1:{api_port}"
    try:
        wait_health(mock_base)
        wait_health(api_base)
    except Exception:
        log.flush()
        failure_log = log_path.read_text(errors="replace")
        cleanup(processes, log, data_dir)
        print(failure_log, file=sys.stderr)
        raise
    return api_base, mock_base, processes, log, data_dir


def cleanup(processes, log, data_dir):
    for process in processes:
        try:
            os.killpg(os.getpgid(process.pid), signal.SIGTERM)
        except ProcessLookupError:
            pass
    for process in processes:
        try:
            process.wait(timeout=5)
        except subprocess.TimeoutExpired:
            try:
                os.killpg(os.getpgid(process.pid), signal.SIGKILL)
            except ProcessLookupError:
                pass
    log.close()
    data_dir.cleanup()


def normalized(value):
    return value.lower().replace("ё", "е")


def build_cases(plan, state):
    steps = plan["steps"]
    access = next(step for step in steps if step["actionType"] == "request_access")
    access_index = steps.index(access)
    next_step = access_index + 1
    while next_step < len(steps) and steps[next_step]["status"] != "current":
        next_step += 1
    next_title = steps[next_step]["title"] if next_step < len(steps) else "Все шаги"
    request = plan.get("request") or {}
    cases = []
    if state == "not_created":
        cases.extend([
            {"name": "greeting", "question": "Привет", "deterministic": True, "sources": False,
             "must_contain_any": ["ai-наставник", "с чего начнем"]},
            {"name": "access", "question": "Какой доступ мне нужен?",
             "must_contain_any": [access["resource"], access["title"], "стенд"],
             "source_ids": ["assigned-plan:", "demo-access-policy"]},
            {"name": "next", "question": "Что дальше по плану?",
             "must_contain_any": [access["title"], access["resource"], "заявк"],
             "source_ids": ["assigned-plan:", "demo-plan-guide"]},
            {"name": "learning", "question": "Что почитать про адаптацию?",
             "source_ids": ["demo-learning-guide"]},
            {"name": "code", "question": "Напиши калькулятор на Python", "deterministic": True,
             "sources": False, "must_contain_any": ["вне моей специализации"]},
            {"name": "offtopic", "question": "Какие скидки в кофейне?", "deterministic": True,
             "sources": False, "must_contain_any": ["нет ответа", "только с адаптацией"]},
        ])
    elif state == "pending":
        cases.extend([
            {"name": "status", "question": "Какой статус моей заявки?", "tool": "get_request_status",
             "deterministic": True, "must_contain": [request.get("id", ""), "ожидает согласования"]},
            {"name": "blocked", "question": "Можно ли переходить к следующему шагу?",
             "must_contain_any": ["ожида", "после соглас"],
             "source_ids": ["assigned-plan:", "demo-plan-guide", "demo-access-policy"]},
            {"name": "communication", "question": "Как договориться о встрече с наставником?",
             "source_ids": ["demo-communication-guide"]},
        ])
    else:
        cases.extend([
            {"name": "status", "question": "Заявка на доступ уже согласована?", "tool": "get_request_status",
             "deterministic": True, "must_contain": [request.get("id", ""), "согласована"]},
            {"name": "next", "question": "Что дальше по плану?",
             "must_contain": [next_title],
             "source_ids": ["assigned-plan:", "demo-plan-guide"]},
            {"name": "learning", "question": "Что почитать про адаптацию?",
             "source_ids": ["demo-learning-guide"]},
        ])
    for case in cases:
        case["deterministic"] = case.get("deterministic", False)
        case["sources"] = case.get("sources", True)
    return cases


def check_case(case, response):
    answer = normalized(response.get("answer") or "")
    sources = [source.get("id", "") for source in response.get("sources") or []]
    checks = {}
    checks["http"] = response.get("status") == 200
    checks["answer"] = bool(answer)
    checks["model"] = not response.get("model") if case["deterministic"] else bool(response.get("model"))
    checks["sources"] = bool(sources) if case["sources"] else not sources
    checks["tool"] = True
    if case.get("tool"):
        checks["tool"] = response.get("toolCall") == case["tool"]
    checks["source_ids"] = True
    for prefix in case.get("source_ids", []):
        if not any(source_id == prefix or source_id.startswith(prefix) for source_id in sources):
            checks["source_ids"] = False
            break
    checks["contains"] = all(normalized(text) in answer for text in case.get("must_contain", []))
    checks["contains_any"] = True
    if case.get("must_contain_any"):
        checks["contains_any"] = any(normalized(text) in answer for text in case["must_contain_any"])
    return checks, all(checks.values())


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument("--positions", default=",".join(POSITIONS))
    parser.add_argument("--states", default="not_created,pending,approved")
    parser.add_argument("--delay", type=float, default=0.5)
    parser.add_argument("--live", action="store_true", help="also call configured model (up to 42 provider calls with one server-side retry)")
    parser.add_argument("--output", default="validation/deterministic.json")
    args = parser.parse_args()
    positions = [item.strip() for item in args.positions.split(",") if item.strip()]
    states = [item.strip() for item in args.states.split(",") if item.strip()]
    if not positions or len(set(positions)) != len(positions) or any(p not in POSITIONS for p in positions):
        parser.error("positions must be distinct members of backend,frontend,analyst")
    ordered_states = ("not_created", "pending", "approved")
    if not states or states != [s for s in ordered_states if s in states]:
        parser.error("states must be distinct and ordered: not_created,pending,approved")
    if args.delay < 0:
        parser.error("--delay must be non-negative")
    if args.live and args.output == "validation/deterministic.json":
        args.output = "validation/live.json"

    api_base, mock_base, processes, log, data_dir = start_services()
    results = []
    stop_reason = None
    try:
        for position in positions:
            if stop_reason:
                break
            status, _ = call(mock_base, f"/demo/interns/intern-1/position", "PUT", {"position": position})
            if status != 200:
                raise RuntimeError(f"cannot switch position to {position}: HTTP {status}")
            status, plan = call(api_base, "/api/plan")
            if status != 200:
                raise RuntimeError(f"cannot load {position} plan: HTTP {status}")
            for state in states:
                if stop_reason:
                    break
                if state == "pending":
                    status, created = call(api_base, "/api/requests", "POST", {},
                                           {"X-Demo-User": "intern-1"})
                    if status not in (200, 201):
                        raise RuntimeError(f"cannot create request for {position}: HTTP {status} {created}")
                if state == "approved":
                    status, current = call(api_base, "/api/requests/current")
                    request = (current or {}).get("request") or {}
                    if not request.get("id"):
                        status, created = call(api_base, "/api/requests", "POST", {},
                                               {"X-Demo-User": "intern-1"})
                        if status not in (200, 201):
                            raise RuntimeError(f"cannot create request for {position}: HTTP {status} {created}")
                        request = (created or {}).get("request") or {}
                    status, approved = call(api_base, f"/api/requests/{request['id']}/approve", "POST",
                                            None, {"X-Demo-User": "mentor-1"})
                    if status != 200:
                        raise RuntimeError(f"cannot approve request for {position}: HTTP {status} {approved}")
                status, plan = call(api_base, "/api/plan")
                if status != 200:
                    raise RuntimeError(f"cannot reload {position} plan: HTTP {status}")
                for case in build_cases(plan, state):
                    if stop_reason:
                        break
                    if not args.live and not case["deterministic"]:
                        continue
                    headers = {"Content-Type": "application/json"}
                    body = {"question": case["question"]}
                    started = time.perf_counter()
                    status_code, payload = call(api_base, "/api/buddy/answer", "POST", body, headers)
                    latency = (time.perf_counter() - started) * 1000
                    payload = payload or {}
                    payload["status"] = status_code
                    checks, passed = check_case(case, payload)
                    results.append({
                        "position": position,
                        "state": state,
                        "name": case["name"],
                        "question": case["question"],
                        "expected_model": not case["deterministic"],
                        "status": status_code,
                        "latency_ms": round(latency, 2),
                        "answer": payload.get("answer", ""),
                        "sources": [source.get("id") for source in payload.get("sources") or []],
                        "model": payload.get("model", ""),
                        "toolCall": payload.get("toolCall", ""),
                        "error": payload.get("error", ""),
                        "checks": checks,
                        "passed": passed,
                    })
                    marker = "PASS" if passed else "FAIL"
                    print(f"{marker} {position}/{state}/{case['name']} {latency:.0f}ms", flush=True)
                    if args.live and not case["deterministic"] and status_code in (429, 503):
                        stop_reason = "Провайдер недоступен или исчерпана квота; прогон остановлен без повторных запросов."
                    time.sleep(args.delay)
    finally:
        cleanup(processes, log, data_dir)

    latencies = [item["latency_ms"] for item in results if item["model"]]
    passed = [item for item in results if item["passed"]]
    model_cases = [item for item in results if item["expected_model"]]
    passed_model = [item for item in model_cases if item["passed"]]
    summary = {
        "generated_at": datetime.now(timezone.utc).isoformat(),
        "mode": "live" if args.live else "deterministic",
        "incomplete_reason": stop_reason,
        "positions": positions,
        "states": states,
        "cases": len(results),
        "passed": len(passed),
        "pass_rate": round(len(passed) / len(results), 4) if results else 0,
        "model_cases": len(model_cases),
        "passed_model_cases": len(passed_model),
        "model_pass_rate": round(len(passed_model) / len(model_cases), 4) if model_cases else None,
        "rate_limited_calls": len([item for item in results if "Лимит запросов" in item.get("error", "")]),
        "model_latency_ms": {
            "median": round(statistics.median(latencies), 2) if latencies else None,
            "p95": round(sorted(latencies)[int(.95 * (len(latencies) - 1))], 2) if latencies else None,
            "max": round(max(latencies), 2) if latencies else None,
        },
        "interpretation": "Автоматический контрактный чек, не оценка смысловой достоверности и не продуктовая метрика.",
    }
    output = {
        "summary": summary,
        "results": results,
    }
    path = ROOT / args.output
    path.parent.mkdir(parents=True, exist_ok=True)
    path.write_text(json.dumps(output, ensure_ascii=False, indent=2) + "\n")
    print(json.dumps(summary, ensure_ascii=False, indent=2))
    if stop_reason:
        return 2
    if len(passed) != len(results):
        return 1
    return 0


if __name__ == "__main__":
    sys.exit(main())
