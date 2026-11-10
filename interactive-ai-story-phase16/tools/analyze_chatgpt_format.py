#!/usr/bin/env python3
"""Summarize a completed format experiment, retaining failed attempts."""
import argparse
from collections import Counter, defaultdict
import json
from math import comb
from pathlib import Path
from statistics import median


def fisher_two_sided(success_a, total_a, success_b, total_b):
    successes = success_a + success_b
    total = total_a + total_b
    def probability(x):
        return comb(total_a, x) * comb(total_b, successes - x) / comb(total, successes)
    observed = probability(success_a)
    return sum(probability(x) for x in range(max(0, successes - total_b), min(total_a, successes) + 1)
               if probability(x) <= observed + 1e-12)


def analyze(root):
    load = lambda name: json.loads((root / name).read_text())
    plan = load('plan.json')
    state = load('state.json')
    if state['status'] != 'complete':
        raise SystemExit('Refusing to present an incomplete series as complete')
    rows = [json.loads(p.read_text()) for p in sorted((root / 'outcomes').glob('*.json'))]
    before = {j['id'] for j in load('bridge-before.json')['jobs']}
    jobs = [j for j in load('bridge-after.json')['jobs'] if j['id'] not in before]
    groups = defaultdict(list)
    for row in rows:
        groups[row['spec']['Format']].append(row)
    result = {'scope': 'Small synthetic diagnostic sample; no quality ranking or rare-failure guarantee',
              'planned': len(plan['schedule']), 'recorded': len(rows), 'formats': {},
              'workerVersions': sorted({j.get('workerVersion', '') for j in jobs}),
              'activeJobsAfter': sum(j['state'] in ('queued', 'leased', 'dispatched') for j in jobs)}
    for fmt, group in groups.items():
        outcomes = [r['outcome'] for r in group]
        ok = [r for r in outcomes if r['OK']]
        related = [j for j in jobs if j.get('inputEncoding', j.get('messageFormat')) == fmt]
        result['formats'][fmt] = {
            'planned': sum(s['Format'] == fmt for s in plan['schedule']),
            'recorded': len(group), 'validJSONObjects': len(ok),
            'errors': dict(Counter(r['ErrorCode'] for r in outcomes if not r['OK'])),
            'completedEmptyUI': sum(j.get('diagnostic', {}).get('stage') == 'completed-empty-ui' for j in related),
            'nonemptyRawDOMOnEmptyUI': sum(j.get('diagnostic', {}).get('stage') == 'completed-empty-ui'
                                        and j.get('diagnostic', {}).get('assistant', {}).get('rawTextLength', 0) > 0
                                        for j in related),
            'medianSuccessfulSeconds': round(median(r['ElapsedMS'] / 1000 for r in ok), 3) if ok else None,
            'maxObservedAttemptSeconds': max(r['ElapsedMS'] / 1000 for r in outcomes),
            'allSuccessfulHTTPPostCounts': sorted({r['HTTPPosts'] for r in ok}),
            'allWorkerJobStates': dict(Counter(j['state'] for j in related))}
    formats = sorted(groups)
    pairs = defaultdict(dict)
    for row in rows:
        pairs[(row['spec']['CaseID'], row['spec']['Repeat'])][row['spec']['Format']] = row['outcome']
    result['logicalInputsMatch'] = all(len({r['InputSHA256'] for r in pair.values()}) == 1 for pair in pairs.values())
    if len(formats) == 2:
        a, b = [result['formats'][f] for f in formats]
        result['twoSidedFisherP'] = fisher_two_sided(a['validJSONObjects'], a['recorded'], b['validJSONObjects'], b['recorded'])
        result['statisticalCaveat'] = 'Repeated scenarios are not independent random user requests; Fisher P is descriptive only. Exploratory multiple comparisons were not corrected.'
    (root / 'analysis.json').write_text(json.dumps(result, ensure_ascii=False, indent=2) + '\n')
    return result


if __name__ == '__main__':
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('root', type=Path)
    args = parser.parse_args()
    print(json.dumps(analyze(args.root), ensure_ascii=False, indent=2))
