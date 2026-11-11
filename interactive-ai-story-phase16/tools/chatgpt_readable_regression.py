#!/usr/bin/env python3
"""One diagnostic replay of a saved synthetic request; never a benchmark replacement."""
import argparse
import datetime as dt
import hashlib
import json
import pathlib
import time
import urllib.error
import urllib.request


def save(path, value):
    path.parent.mkdir(parents=True, exist_ok=True)
    path.write_text(json.dumps(value, ensure_ascii=False, indent=2) + '\n')


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--snapshot', type=pathlib.Path, required=True)
    parser.add_argument('--job-prefix', required=True)
    parser.add_argument('--output', type=pathlib.Path, required=True)
    args = parser.parse_args()
    root = pathlib.Path(__file__).resolve().parents[1]
    token = (root / 'data/secrets/chatgpt-bridge-token').read_text().strip()
    headers = {'Authorization': 'Bearer ' + token, 'Content-Type': 'application/json',
               'X-Story-Benchmark': 'paired-v1'}

    def status():
        request = urllib.request.Request('http://127.0.0.1:8791/status', headers=headers)
        with urllib.request.urlopen(request, timeout=5) as response:
            return json.load(response)

    candidates = [j for j in json.loads(args.snapshot.read_text())['jobs']
                  if j['id'].startswith(args.job_prefix)]
    if len(candidates) != 1:
        raise SystemExit('Expected exactly one saved synthetic request')
    job = candidates[0]
    envelope = json.loads(job['messageContent'])
    messages = [{'role': m['role'], 'content': m['content'] if isinstance(m['content'], str)
                 else json.dumps(m['content'], ensure_ascii=False, separators=(',', ':'))}
                for m in envelope['messages']]
    payload = json.dumps({'model': 'gpt-6-instant', 'messages': messages,
                          'response_format': envelope.get('response_format'), 'max_tokens': 4096},
                         ensure_ascii=False).encode()
    before = status()
    if any(j['state'] in ('queued', 'leased', 'dispatched') for j in before['jobs']):
        raise SystemExit('Another request is active; no replay sent')
    now = dt.datetime.now(dt.timezone.utc)
    ready = [w for w in before['workers'].values() if w.get('version') in ('0.2.2','0.3.0')
             and w.get('status') == 'ready'
             and (now - dt.datetime.fromisoformat(w['seen'].replace('Z', '+00:00'))).total_seconds() < 15]
    if not ready:
        raise SystemExit('No ready compatible worker; no replay sent')
    if args.output.exists():
        raise SystemExit('Output already exists; refusing duplicate replay')
    args.output.parent.mkdir(parents=True, exist_ok=True)
    started = pathlib.Path(str(args.output) + '.started')
    with started.open('x') as f:
        json.dump({'sourceJobPrefix': args.job_prefix, 'started': now.isoformat()}, f)
    save(pathlib.Path(str(args.output) + '.bridge-before.json'), before)
    row = {'scope': 'Independent diagnostic replay, not a replacement for a failed benchmark attempt',
           'sourceJobPrefix': args.job_prefix, 'workerVersion': ready[0]['version'],
           'requestSHA256': hashlib.sha256(payload).hexdigest(), 'started': now.isoformat()}
    start = time.monotonic()
    request = urllib.request.Request('http://127.0.0.1:8791/v1/chat/completions',
                                     data=payload, headers=headers, method='POST')
    try:
        with urllib.request.urlopen(request, timeout=170) as response:
            row['httpStatus'] = response.status
            response_body = json.load(response)
        content = response_body['choices'][0]['message']['content']
        answer = json.loads(content)
        row.update(validJSON=isinstance(answer, dict), output=answer,
                   hasText=isinstance(answer.get('text'), str) and bool(answer['text'].strip()))
    except urllib.error.HTTPError as error:
        row.update(httpStatus=error.code, error='bridge_rejected_result')
    except (OSError, ValueError, KeyError, IndexError):
        row.update(error='transport_or_invalid_response')
    finally:
        row['elapsedSeconds'] = round(time.monotonic() - start, 3)
        save(args.output, row)
        save(pathlib.Path(str(args.output) + '.bridge-after.json'), status())
    print(json.dumps({k: row.get(k) for k in ('httpStatus', 'validJSON', 'hasText', 'elapsedSeconds', 'error')}))
    return 0 if row.get('validJSON') and row.get('hasText') else 1


if __name__ == '__main__':
    raise SystemExit(main())
