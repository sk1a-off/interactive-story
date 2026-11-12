#!/usr/bin/env python3
"""Synthetic text-only bridge probe; no file uploads and no retries of failed trials."""
import argparse
import datetime as dt
import hashlib
import json
from pathlib import Path
import secrets
import time
import urllib.error
import urllib.request

ROOT = Path(__file__).resolve().parents[1]


def main():
    p = argparse.ArgumentParser(description=__doc__)
    p.add_argument('--sizes', default='8000,16000,32000,64000,128000')
    p.add_argument('--trials', type=int, choices=[1, 2, 3], default=2)
    p.add_argument('--report', type=Path, required=True)
    args = p.parse_args()
    sizes = list(map(int, args.sizes.split(',')))
    if not sizes or any(n < 1000 or n > 500000 for n in sizes):
        p.error('sizes must be 1000..500000 characters')
    if args.report.exists():
        p.error('report exists; use a new path to preserve previous failures')
    token = (ROOT / 'data/secrets/chatgpt-bridge-token').read_text().strip()
    headers = {'Authorization': 'Bearer ' + token, 'Content-Type': 'application/json', 'X-Story-Project': '1'}

    def api(path, body=None):
        data = json.dumps(body, ensure_ascii=False).encode() if body is not None else None
        with urllib.request.urlopen(urllib.request.Request('http://127.0.0.1:8791' + path, data=data, headers=headers), timeout=720 if body else 10) as r:
            return json.load(r)

    rows = []
    report = {'synthetic': True, 'provider': 'chatgpt_browser', 'model': 'gpt-6', 'reasoning': 'none', 'worker': '0.4.1', 'started': dt.datetime.now(dt.timezone.utc).isoformat(), 'rows': rows,
              'interpretation': 'Input UTF-8 bytes and Unicode/UTF-16 lengths are measured, not model tokens. Success checks five anchors, not understanding of every record. Largest success is a lower bound; UI failures do not establish model context limits.'}
    args.report.parent.mkdir(parents=True, exist_ok=True)

    def save():
        args.report.write_text(json.dumps(report, ensure_ascii=False, indent=2) + '\n')

    for target in sizes:
        for trial in range(1, args.trials + 1):
            status = api('/status')
            now = dt.datetime.now(dt.timezone.utc)
            ready = [w for w in status['workers'].values() if w.get('version') == '0.4.1' and w.get('status') == 'ready' and (now - dt.datetime.fromisoformat(w['seen'])).total_seconds() < 15]
            if status.get('project_input_format') != 'text' or not ready or any(j['state'] in ('queued', 'leased', 'dispatched') for j in status['jobs']):
                raise SystemExit('Text bridge or ready worker unavailable, or another job is active; no new request sent')
            nonce = secrets.token_hex(12)
            expected = [secrets.token_hex(12) for _ in range(5)]
            pad_size = max(0, (target - 500) // 4)
            sections = []
            for n, value in enumerate(expected):
                padding = ''
                if n < 4:
                    records = []
                    length = 0
                    while length < pad_size:
                        record = 'Архив: северная лестница, стекло, следы, ' + secrets.token_hex(8) + '. '
                        records.append(record)
                        length += len(record)
                    padding = ''.join(records)[:pad_size]
                sections.append({'position': n, 'anchor': value, 'padding': padding})
            content = json.dumps({'probe': nonce, 'sections': sections}, ensure_ascii=False, separators=(',', ':'))
            row = {'targetCharacters': target, 'trial': trial, 'nonce': nonce, 'inputCharacters': len(content), 'inputBytes': len(content.encode()), 'inputSHA256': hashlib.sha256(content.encode()).hexdigest(), 'expectedValues': expected, 'ok': False, 'classification': 'pending'}
            rows.append(row); save()
            started = time.monotonic()
            try:
                response = api('/v1/chat/completions', {'model': 'gpt-6', 'reasoning_effort': 'none', 'response_format': {'type': 'json_object'}, 'messages': [
                    {'role': 'system', 'content': 'Synthetic context retrieval test. Read every section. Return ONLY one JSON object {"values":[...]} containing the five anchor strings from sections 0 through 4 in order. Copy exactly. If unavailable use null. Do not browse or use tools.'}, {'role': 'user', 'content': content}]})
                row['jobId'] = response['id']
                row['actualValues'] = json.loads(response['choices'][0]['message']['content']).get('values')
                row['classification'] = 'anchors_match' if row['actualValues'] == expected else 'retrieval_failed'
            except urllib.error.HTTPError as e:
                row.update(classification='bridge_or_ui_error', httpStatus=e.code, error=e.read(800).decode(errors='replace'))
            except Exception as e:
                row.update(classification='transport_or_output_error', error=str(e))
            try:
                latest = api('/status')
                matches = [j for j in latest['jobs'] if nonce in j.get('prompt', '')]
                if len(matches) == 1:
                    job = matches[0]
                    row['job'] = {k: job.get(k) for k in ('id', 'state', 'workerVersion', 'textOnly', 'textOnlyVerified', 'sent', 'finished', 'error', 'diagnostic')}
                    prompt = job['prompt']
                    row.update(promptCharacters=len(prompt), promptUTF16Units=len(prompt.encode('utf-16-le')) // 2, promptBytes=len(prompt.encode()), attachmentPresent=job.get('attachment') is not None)
                    row['ok'] = row.get('actualValues') == expected and job.get('textOnlyVerified') is True and not row['attachmentPresent'] and job['state'] == 'complete'
                    if row.get('actualValues') == expected and not row['ok']:
                        row['classification'] = 'text_only_verification_failed'
            except Exception as e:
                row['metadataError'] = str(e)
            row['elapsedSeconds'] = round(time.monotonic() - started, 3); save()
            print(json.dumps({k: row.get(k) for k in ('targetCharacters', 'trial', 'ok', 'classification', 'promptCharacters', 'attachmentPresent', 'elapsedSeconds')}), flush=True)
            if not row['ok']:
                return 1
    return 0


if __name__ == '__main__':
    raise SystemExit(main())
