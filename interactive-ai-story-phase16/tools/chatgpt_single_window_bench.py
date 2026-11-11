#!/usr/bin/env python3
"""Sequential browser test using frozen synthetic messages; no database or model-setting changes."""
import argparse
import datetime as dt
import hashlib
import json
from pathlib import Path
import random
import time
import urllib.error
import urllib.request

from chatgpt_readable_regression import save

ROOT = Path(__file__).resolve().parents[1]
BENCH = ROOT / 'docs/implementation/benchmarks'


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--output', type=Path, required=True)
    parser.add_argument('--repeats', type=int, default=4)
    parser.add_argument('--seed', type=int, default=20261111)
    args = parser.parse_args()
    if not 1 <= args.repeats <= 20:
        raise SystemExit('repeats must be 1..20')
    source = BENCH / 'chatgpt-readable-json-worker022-2026-11-10/bridge-after.json'
    corpus = json.loads((BENCH / 'paired-chatgpt-antigravity-readable022-2026-11-10/plan.json').read_text())
    jobs = json.loads(source.read_text())['jobs']
    fixtures = {}
    for case_id in ('turn_planner_browser', 'writer_archive', 'writer_detective'):
        case = next(c for c in corpus['Cases'] if c['ID'] == case_id)
        for job in jobs:
            if not job.get('messageContent'):
                continue
            envelope = json.loads(job['messageContent'])
            inputs = [m['content'] for m in envelope['messages'] if m['role'] == 'user']
            if inputs == [case['Input']]:
                fixtures[case_id] = envelope
                break
        if case_id not in fixtures:
            raise SystemExit('Frozen messages unavailable: ' + case_id)
    token = (ROOT / 'data/secrets/chatgpt-bridge-token').read_text().strip()
    headers = {'Authorization': 'Bearer ' + token, 'Content-Type': 'application/json',
               'X-Story-Benchmark': 'paired-v1'}

    def status():
        req = urllib.request.Request('http://127.0.0.1:8791/status', headers=headers)
        with urllib.request.urlopen(req, timeout=5) as response:
            return json.load(response)

    def wait_ready(expected=None):
        deadline = time.monotonic() + 30
        while time.monotonic() < deadline:
            state = status()
            now = dt.datetime.now(dt.timezone.utc)
            live = {worker: w for worker, w in state['workers'].items()
                    if w.get('status') in ('ready', 'busy') and
                    (now - dt.datetime.fromisoformat(w['seen'].replace('Z', '+00:00'))).total_seconds() < 15}
            if len(live) > 1:
                raise RuntimeError('More than one active worker; no next request sent')
            if any(j['state'] in ('queued', 'leased', 'dispatched') for j in state['jobs']):
                raise RuntimeError('Another request is active; no next request sent')
            if live:
                worker, info = next(iter(live.items()))
                if expected and worker != expected:
                    raise RuntimeError('Worker changed; no next request sent')
                if info.get('version') not in ('0.2.2','0.3.0'):
                    raise RuntimeError('Expected compatible ChatGPT worker')
                if info['status'] == 'ready':
                    return worker, state
            time.sleep(1)
        raise RuntimeError('Selected worker not ready; remaining requests not sent')

    worker, before = wait_ready()
    schedule = [{'case': case_id, 'repeat': repeat} for case_id in fixtures
                for repeat in range(1, args.repeats + 1)]
    random.Random(args.seed).shuffle(schedule)
    args.output.mkdir(parents=True, exist_ok=False)
    save(args.output / 'bridge-before.json', before)
    save(args.output / 'plan.json', {'worker': worker, 'workers': 1, 'parallelRequests': 1,
         'seed': args.seed, 'promptRevision': corpus['PromptRevision'], 'schedule': schedule,
         'source': str(source.relative_to(ROOT)), 'sourceSHA256': hashlib.sha256(source.read_bytes()).hexdigest(),
         'scope': 'Same three synthetic scenarios as the previous format study; readable-json-v3 only. Independent sequential series; no retries or silent worker replacement.'})
    rows, run_state, reason = [], 'running', ''
    save(args.output / 'state.json', {'status': run_state})
    try:
        for item in schedule:
            wait_ready(worker)
            envelope = fixtures[item['case']]
            messages = [{'role': m['role'], 'content': m['content'] if isinstance(m['content'], str)
                         else json.dumps(m['content'], ensure_ascii=False, separators=(',', ':'))}
                        for m in envelope['messages']]
            payload = json.dumps({'model': 'gpt-6-instant', 'messages': messages, 'max_tokens': 4096,
                                  'response_format': envelope.get('response_format')}, ensure_ascii=False).encode()
            old_ids = {j['id'] for j in status()['jobs']}
            row = dict(item, started=dt.datetime.now(dt.timezone.utc).isoformat(), validJSON=False,
                       inputSHA256=hashlib.sha256(payload).hexdigest())
            rows.append(row)
            save(args.output / 'results.json', rows)
            start = time.monotonic()
            req = urllib.request.Request('http://127.0.0.1:8791/v1/chat/completions',
                                         data=payload, headers=headers, method='POST')
            try:
                with urllib.request.urlopen(req, timeout=170) as response:
                    row['httpStatus'] = response.status
                    content = json.load(response)['choices'][0]['message']['content']
                row['output'] = json.loads(content)
                row['validJSON'] = isinstance(row['output'], dict) and bool(row['output'])
                if item['case'].startswith('writer'):
                    row['hasText'] = isinstance(row['output'].get('text'), str) and bool(row['output']['text'].strip())
            except urllib.error.HTTPError as error:
                row.update(httpStatus=error.code, error='bridge_rejected_result')
            except (OSError, ValueError, KeyError, IndexError, TypeError):
                row['error'] = 'transport_or_invalid_response'
            finally:
                row['elapsedSeconds'] = round(time.monotonic() - start, 3)
                after = status()
                observed = [j for j in after['jobs'] if j['id'] not in old_ids]
                row['jobs'] = [{'id': j['id'], 'worker': j.get('worker'), 'state': j['state'],
                                'inputEncoding': j.get('inputEncoding'), 'diagnostic': j.get('diagnostic')}
                               for j in observed]
                save(args.output / 'results.json', rows)
                save(args.output / 'bridge-after.json', after)
            if len(observed) != 1 or observed[0].get('worker') != worker:
                raise RuntimeError('Unexpected job or worker observed; no next request sent')
            print(f"case={item['case']} repeat={item['repeat']} json={row['validJSON']} state={observed[0]['state']} seconds={row['elapsedSeconds']}", flush=True)
        run_state = 'complete'
    except (RuntimeError, OSError) as error:
        run_state, reason = 'stopped', str(error) if isinstance(error, RuntimeError) else 'local bridge unavailable'
        print(reason, flush=True)
    except KeyboardInterrupt:
        run_state, reason = 'interrupted', 'Caller interrupted; no request will be retried'
    finally:
        save(args.output / 'state.json', {'status': run_state, 'reason': reason,
             'planned': len(schedule), 'recorded': len(rows), 'finished': dt.datetime.now(dt.timezone.utc).isoformat()})
    return 0 if run_state == 'complete' else 1


if __name__ == '__main__':
    raise SystemExit(main())
