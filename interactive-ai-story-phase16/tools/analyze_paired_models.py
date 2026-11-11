#!/usr/bin/env python3
"""Analyze the predeclared paired model study, including failures and missing data."""
import argparse
from collections import Counter, defaultdict
import json
import math
from pathlib import Path
import random
from statistics import mean, median

ARMS = ('chatgpt', 'antigravity')


def quantile(values, p):
    values = sorted(values)
    if not values:
        return None
    i = (len(values) - 1) * p
    lower = int(i)
    return values[lower] + (values[min(lower + 1, len(values) - 1)] - values[lower]) * (i - lower)


def clustered_interval(rows, statistic=mean, iterations=10000, seed=20261110):
    groups = defaultdict(list)
    for family, value in rows:
        groups[family].append(value)
    if not rows:
        return None
    keys = sorted(groups)
    rng = random.Random(seed)
    draws = []
    for _ in range(iterations):
        sample = [value for key in rng.choices(keys, k=len(keys)) for value in groups[key]]
        draws.append(statistic(sample))
    return {'estimate': statistic([value for _, value in rows]),
            'ci95': [quantile(draws, .025), quantile(draws, .975)],
            'pairs': len(rows), 'scenarioFamilies': len(groups)}


def winner(delta, threshold=.20):
    return 'chatgpt' if delta > threshold else 'antigravity' if delta < -threshold else 'tie'


def analyze(root, partial=False):
    load = lambda name: json.loads((root / name).read_text())
    plan, state, spec = load('plan.json'), load('state.json'), load('analysis-spec.json')
    if state.get('status') != 'complete' and not partial:
        raise SystemExit('Study incomplete; use --partial to explicitly label an interim analysis')
    cases = {c['ID']: c for c in plan['Cases']}
    pairs = [load('pairs/' + s['ID'] + '.json') for s in plan['Schedule']
             if (root / 'pairs' / (s['ID'] + '.json')).exists()]
    weights = spec['writerWeights']
    result = {'status': state, 'scope': plan['Notes'], 'planVersion': plan['Version'],
              'promptRevision': plan['PromptRevision'], 'configRevision': plan['ConfigRevision'],
              'plannedPairs': len(plan['Schedule']), 'recordedPairs': len(pairs),
              'missingPairs': len(plan['Schedule']) - len(pairs), 'arms': {}, 'latency': {}, 'quality': {}, 'analysisRevision': 2, 'scoreInterpretation': 'languageScore is a continuous character ratio; strictAllScoresPerfect is not a production acceptance gate. schemaInstructionContinuityPass excludes that language ratio.'}
    hashes = [p['Outcomes'][a].get('InputSHA256') for p in pairs for a in ARMS]
    result['logicalInputsMatch'] = all(p['Outcomes']['chatgpt'].get('InputSHA256') ==
                                       p['Outcomes']['antigravity'].get('InputSHA256') for p in pairs)
    result['hashesPresent'] = bool(hashes) and all(hashes)
    for group in ('all', 'writer', 'structured'):
        subset = [p for p in pairs if group == 'all' or cases[p['Spec']['CaseID']]['Writer'] == (group == 'writer')]
        planned = sum(group == 'all' or cases[s['CaseID']]['Writer'] == (group == 'writer') for s in plan['Schedule'])
        group_summary = {'planned': planned, 'recorded': len(subset)}
        for arm in ARMS:
            rows = [p['Outcomes'][arm] for p in subset]
            ok = [r for r in rows if r['OK']]
            seconds = [r['ElapsedMS'] / 1000 for r in ok]
            mechanical = [r for r in ok if all(r['Checks'].get(k) == 1 for k in
                          ('schemaScore', 'instructionScore', 'languageScore', 'continuityScore'))]
            group_summary[arm] = {'validJSONObjects': len(ok), 'strictAllScoresPerfect': len(mechanical),
                                  'schemaInstructionContinuityPass': sum(all(r['Checks'].get(k) == 1 for k in ('schemaScore', 'instructionScore', 'continuityScore')) for r in ok),
                                  'errors': dict(Counter(r['ErrorCode'] for r in rows if not r['OK'])),
                                  'medianSuccessfulSeconds': median(seconds) if seconds else None,
                                  'p90SuccessfulSeconds': quantile(seconds, .90),
                                  'actualHTTPPosts': sum(r.get('HTTPPosts', 0) for r in rows),
                                  'medianObservedAttemptSeconds': median([r['ElapsedMS'] / 1000 for r in rows]) if rows else None}
        result['arms'][group] = group_summary
        conditional, penalized = [], []
        for pair in subset:
            family = cases[pair['Spec']['CaseID']]['Family']
            a, b = [pair['Outcomes'][arm] for arm in ARMS]
            if a['OK'] and b['OK'] and a['ElapsedMS'] > 0 and b['ElapsedMS'] > 0:
                conditional.append((family, math.log(a['ElapsedMS'] / b['ElapsedMS'])))
            budget = spec.get('failurePenaltySeconds', plan['CallDeadlineSeconds'])
            sa = a['ElapsedMS'] / 1000 if a['OK'] and a['ElapsedMS'] > 0 else budget
            sb = b['ElapsedMS'] / 1000 if b['OK'] and b['ElapsedMS'] > 0 else budget
            penalized.append((family, math.log(sa / sb)))
        result['latency'][group] = {}
        for label, rows in [('bothValidOnly', conditional), ('failurePenaltySensitivity', penalized)]:
            estimate = clustered_interval(rows, median, spec['bootstrapIterations'], spec['bootstrapSeed'])
            if estimate:
                estimate.update(ratio=math.exp(estimate['estimate']), ratioCI95=[math.exp(v) for v in estimate['ci95']])
            result['latency'][group][label] = estimate
    verdicts, primary = defaultdict(list), []
    by_judge = defaultdict(list)
    raw_winners = defaultdict(Counter)
    critical_mentions = defaultdict(Counter)
    dimension_scores = {arm: defaultdict(list) for arm in ARMS}
    flags = []
    attempted_judges = valid_judges = 0
    for path in sorted((root / 'judgments').glob('*.json')):
        j = json.loads(path.read_text())
        if 'judge' not in j:
            continue
        attempted_judges += 1
        if not j.get('validVerdict'):
            continue
        valid_judges += 1
        v, judge = j['outcome']['Output'], j['judge']
        a_arm, b_arm = j['aArm'], j['bArm']
        score = {a_arm: sum(v['A'][k] * w for k, w in weights.items()),
                 b_arm: sum(v['B'][k] * w for k, w in weights.items())}
        delta = score['chatgpt'] - score['antigravity']
        verdicts[j['pair']].append((judge, delta))
        by_judge[judge].append(delta)
        declared = {'A': a_arm, 'B': b_arm, 'tie': 'tie'}[v['winner']]
        raw_winners[judge][declared] += 1
        for label, arm in [('A', a_arm), ('B', b_arm)]:
            for key in weights:
                dimension_scores[arm][key].append(v[label][key])
            errors = v.get('criticalErrors' + label) or []
            if errors:
                critical_mentions[judge][arm] += 1
                flags.append({'pair': j['pair'], 'judge': judge, 'arm': arm, 'errors': errors,
                              'interpretation': 'Judge allegation with quote validation; not independent human adjudication'})
    agreement = 0
    for pair in pairs:
        pair_id = pair['Spec']['ID']
        ratings = verdicts.get(pair_id, [])
        if len(ratings) == 2 and {j for j, _ in ratings} == set(ARMS):
            delta = mean(d for _, d in ratings)
            family = cases[pair['Spec']['CaseID']]['Family']
            primary.append((family, delta))
            agreement += winner(ratings[0][1]) == winner(ratings[1][1])
    eligible_writers = sum(cases[p['Spec']['CaseID']]['Writer'] and all(p['Outcomes'][a]['OK'] for a in ARMS) for p in pairs)
    result['quality'] = {'eligibleWriterPairsBothOutputs': eligible_writers,
                         'judgeRecords': attempted_judges, 'validVerdicts': valid_judges,
                         'primaryPairsWithTwoValidJudges': len(primary),
                         'primaryWeightedDeltaCGminusAG': clustered_interval(primary, mean, spec['bootstrapIterations'], spec['bootstrapSeed']),
                         'primaryWeightedWinners': dict(Counter(winner(d) for _, d in primary)),
                         'weightedJudgeAgreementPairs': agreement,
                         'perJudgeMeanDelta': {j: mean(ds) for j, ds in by_judge.items()},
                         'declaredWinnersByJudge': {j: dict(c) for j, c in raw_winners.items()},
                         'criticalErrorAllegationsByJudge': {j: dict(c) for j, c in critical_mentions.items()},
                         'dimensionMeansAcrossValidJudgments': {a: {k: mean(v) for k, v in ds.items()} for a, ds in dimension_scores.items()},
                         'criticalErrorFlags': flags,
                         'caveat': 'Only two valid judges per pair enter primary quality; failed deliveries and missing judgments are reported separately. Self-preference and common rubric bias remain. Scenario families are fixed, not a random user sample.'}
    reliability_rows, mean_penalty_differences = [], []
    case_results = []
    for pair in pairs:
        family = cases[pair['Spec']['CaseID']]['Family']
        cg, ag = [pair['Outcomes'][arm] for arm in ARMS]
        reliability_rows.append((family, int(cg['OK']) - int(ag['OK'])))
        penalty_values = [r['ElapsedMS'] / 1000 if r['OK'] else plan['CallDeadlineSeconds'] for r in (cg, ag)]
        mean_penalty_differences.append((family, penalty_values[0] - penalty_values[1]))
    result['exploratoryReliabilityDifferenceCGminusAG'] = clustered_interval(reliability_rows, mean, spec['bootstrapIterations'], spec['bootstrapSeed'])
    result['exploratoryMeanPenaltyDifferenceSecondsCGminusAG'] = clustered_interval(mean_penalty_differences, mean, spec['bootstrapIterations'], spec['bootstrapSeed'])
    for case_id, case in cases.items():
        subset = [p for p in pairs if p['Spec']['CaseID'] == case_id]
        row = {'case': case_id, 'writer': case['Writer'], 'recorded': len(subset)}
        for arm in ARMS:
            ok = [p['Outcomes'][arm] for p in subset if p['Outcomes'][arm]['OK']]
            times = [r['ElapsedMS'] / 1000 for r in ok]
            prose = [r['Prose'] for r in ok if r.get('Prose')]
            row[arm] = {'validJSONObjects': len(ok), 'medianSuccessfulSeconds': median(times) if times else None,
                        'strictAllScoresPerfect': sum(all(r['Checks'].get(k) == 1 for k in ('schemaScore', 'instructionScore', 'languageScore', 'continuityScore')) for r in ok),
                        'schemaInstructionContinuityPass': sum(all(r['Checks'].get(k) == 1 for k in ('schemaScore', 'instructionScore', 'continuityScore')) for r in ok),
                        'medianWriterWords': median(r['words'] for r in prose) if prose else None,
                        'medianWriterParagraphs': median(r['paragraphs'] for r in prose) if prose else None}
        case_results.append(row)
    result['cases'] = case_results
    result['exploratoryCaveat'] = 'Reliability difference, mean penalty difference and case breakdown are exploratory. Failed outputs cost the predeclared 180-second penalty; this is not observed waiting time.'
    result['fullTurns'] = [json.loads(p.read_text()) for p in sorted((root / 'turns').glob('*.json'))]
    result['bridgeJobStatesByPhase'] = {}
    before_path = root / 'bridge-before.json'
    seen_jobs = {j['id'] for j in json.loads(before_path.read_text())['jobs']} if before_path.exists() else set()
    for phase in ('roles', 'judges', 'after'):
        path = root / ('bridge-' + phase + '.json')
        if path.exists():
            snapshot_jobs = json.loads(path.read_text())['jobs']
            jobs = [j for j in snapshot_jobs if j['id'] not in seen_jobs]
            seen_jobs.update(j['id'] for j in snapshot_jobs)
            result['bridgeJobStatesByPhase'][phase] = dict(Counter((j.get('diagnostic') or {}).get('stage', j['state']) for j in jobs))
    (root / ('analysis-partial.json' if partial else 'analysis.json')).write_text(json.dumps(result, ensure_ascii=False, indent=2) + '\n')
    return result


if __name__ == '__main__':
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('root', type=Path)
    parser.add_argument('--partial', action='store_true')
    args = parser.parse_args()
    summary = analyze(args.root, args.partial)
    print(json.dumps({k: summary[k] for k in ('status', 'plannedPairs', 'recordedPairs', 'arms', 'latency')}, ensure_ascii=False, indent=2))
